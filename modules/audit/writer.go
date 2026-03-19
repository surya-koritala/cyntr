package audit

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

type Writer struct {
	mu       sync.Mutex
	db       *sql.DB
	instance string
	secret   []byte
	prevHash string
}

func NewWriter(dbPath, instance, secret string) (*Writer, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open audit db: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audit_entries (
			id TEXT PRIMARY KEY,
			timestamp TEXT NOT NULL,
			instance TEXT NOT NULL,
			tenant TEXT NOT NULL,
			data TEXT NOT NULL,
			signature TEXT NOT NULL,
			prev_hash TEXT NOT NULL,
			entry_hash TEXT NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	var prevHash string
	err = db.QueryRow("SELECT entry_hash FROM audit_entries ORDER BY rowid DESC LIMIT 1").Scan(&prevHash)
	if err == sql.ErrNoRows {
		prevHash = "genesis"
	} else if err != nil {
		db.Close()
		return nil, fmt.Errorf("load last hash: %w", err)
	}

	return &Writer{db: db, instance: instance, secret: []byte(secret), prevHash: prevHash}, nil
}

func (w *Writer) Write(entry Entry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	entry.Instance = w.instance
	entry.PrevHash = w.prevHash

	// Marshal for hashing (signature empty at this point)
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}

	// Hash: SHA256(prev_hash + entry_data)
	h := sha256.New()
	h.Write([]byte(w.prevHash))
	h.Write(data)
	entryHash := hex.EncodeToString(h.Sum(nil))

	// Sign with HMAC
	mac := hmac.New(sha256.New, w.secret)
	mac.Write([]byte(entryHash))
	signature := hex.EncodeToString(mac.Sum(nil))

	entry.Signature = signature

	// Re-marshal with signature included
	data, err = json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal signed entry: %w", err)
	}

	_, err = w.db.Exec(
		"INSERT INTO audit_entries (id, timestamp, instance, tenant, data, signature, prev_hash, entry_hash) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		entry.ID, entry.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z"),
		entry.Instance, entry.Tenant, string(data), signature, w.prevHash, entryHash,
	)
	if err != nil {
		return fmt.Errorf("insert entry: %w", err)
	}

	w.prevHash = entryHash
	return nil
}

func (w *Writer) VerifyChain() error {
	rows, err := w.db.Query("SELECT id, data, prev_hash, entry_hash FROM audit_entries ORDER BY rowid ASC")
	if err != nil {
		return fmt.Errorf("query entries: %w", err)
	}
	defer rows.Close()

	expectedPrev := "genesis"
	for rows.Next() {
		var id, data, prevHash, entryHash string
		if err := rows.Scan(&id, &data, &prevHash, &entryHash); err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		if prevHash != expectedPrev {
			return fmt.Errorf("chain broken at %s: prev_hash mismatch", id)
		}

		// Recompute hash from stored data (which was marshaled BEFORE signing)
		// We need to unmarshal, clear the signature, and re-marshal to get the pre-signing data
		var entry Entry
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			return fmt.Errorf("unmarshal %s: %w", id, err)
		}
		entry.Signature = ""
		preSignData, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("remarshal %s: %w", id, err)
		}

		h := sha256.New()
		h.Write([]byte(prevHash))
		h.Write(preSignData)
		computed := hex.EncodeToString(h.Sum(nil))

		if computed != entryHash {
			return fmt.Errorf("chain broken at %s: entry_hash mismatch (computed %s, stored %s)", id, computed, entryHash)
		}
		expectedPrev = entryHash
	}
	return rows.Err()
}

func (w *Writer) Close() error {
	return w.db.Close()
}
