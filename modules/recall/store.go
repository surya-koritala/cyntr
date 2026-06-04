package recall

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"

	_ "modernc.org/sqlite"
)

// Store persists indexed messages and per-session summaries.
//
// recall_messages is the source of truth (also the FTS5 external-content
// table); recall_fts is the full-text index over its `text` column. Searching
// joins back to recall_messages so the tenant/user filter runs on a real,
// indexed table — tenant isolation is never delegated to the FTS layer.
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// NewStore opens (or creates) a recall store at dbPath.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open recall db: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS recall_messages (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			msg_id     TEXT,
			tenant     TEXT NOT NULL,
			user       TEXT NOT NULL,
			session    TEXT NOT NULL,
			role       TEXT NOT NULL,
			text       TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_recall_scope ON recall_messages(tenant, user, session, id)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS recall_fts USING fts5(text, content='recall_messages', content_rowid='id')`,
		`CREATE TABLE IF NOT EXISTS session_summaries (
			tenant        TEXT NOT NULL,
			user          TEXT NOT NULL,
			session       TEXT NOT NULL,
			summary       TEXT NOT NULL,
			message_count INTEGER NOT NULL DEFAULT 0,
			updated_at    TEXT NOT NULL,
			PRIMARY KEY (tenant, user, session)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			db.Close()
			return nil, fmt.Errorf("recall schema: %w", err)
		}
	}
	return &Store{db: db}, nil
}

// Index adds one message to the store and its full-text index.
func (s *Store) Index(m IndexedMessage) error {
	if m.Tenant == "" || m.User == "" || m.Session == "" {
		return fmt.Errorf("recall: tenant, user and session are required")
	}
	if strings.TrimSpace(m.Text) == "" {
		return nil // nothing to index
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`INSERT INTO recall_messages (msg_id, tenant, user, session, role, text, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.MsgID, m.Tenant, m.User, m.Session, m.Role, m.Text, m.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("recall: index insert: %w", err)
	}
	rowid, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("recall: index rowid: %w", err)
	}
	// Keep the external-content FTS index in sync with the matching rowid.
	if _, err := s.db.Exec(`INSERT INTO recall_fts (rowid, text) VALUES (?, ?)`, rowid, m.Text); err != nil {
		return fmt.Errorf("recall: fts insert: %w", err)
	}
	return nil
}

// Search returns up to limit past messages for (tenant, user) matching query,
// best match first. An empty/whitespace query returns no results.
func (s *Store) Search(tenant, user, query string, limit int) ([]Snippet, error) {
	if tenant == "" || user == "" {
		return nil, fmt.Errorf("recall: tenant and user are required")
	}
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(
		`SELECT m.session, m.role, m.text, m.created_at, bm25(recall_fts) AS score
		 FROM recall_fts
		 JOIN recall_messages m ON m.id = recall_fts.rowid
		 WHERE recall_fts MATCH ? AND m.tenant = ? AND m.user = ?
		 ORDER BY score ASC
		 LIMIT ?`,
		match, tenant, user, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("recall: search: %w", err)
	}
	defer rows.Close()

	var out []Snippet
	for rows.Next() {
		var sn Snippet
		var createdAt string
		if err := rows.Scan(&sn.Session, &sn.Role, &sn.Text, &createdAt, &sn.Score); err != nil {
			return nil, fmt.Errorf("recall: scan: %w", err)
		}
		sn.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		out = append(out, sn)
	}
	return out, rows.Err()
}

// SessionMessages returns the most recent `limit` messages of a session in
// chronological order, plus the total message count for the session. Used to
// build the transcript handed to the summarizer.
func (s *Store) SessionMessages(tenant, user, session string, limit int) ([]Snippet, int, error) {
	if limit <= 0 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var total int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM recall_messages WHERE tenant=? AND user=? AND session=?`,
		tenant, user, session,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("recall: count: %w", err)
	}

	rows, err := s.db.Query(
		`SELECT session, role, text, created_at FROM recall_messages
		 WHERE tenant=? AND user=? AND session=?
		 ORDER BY id DESC LIMIT ?`,
		tenant, user, session, limit,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("recall: session messages: %w", err)
	}
	defer rows.Close()

	var recent []Snippet
	for rows.Next() {
		var sn Snippet
		var createdAt string
		if err := rows.Scan(&sn.Session, &sn.Role, &sn.Text, &createdAt); err != nil {
			return nil, 0, err
		}
		sn.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		recent = append(recent, sn)
	}
	// Reverse to chronological order.
	for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
		recent[i], recent[j] = recent[j], recent[i]
	}
	return recent, total, rows.Err()
}

// MessageCount returns how many messages a session currently has.
func (s *Store) MessageCount(tenant, user, session string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM recall_messages WHERE tenant=? AND user=? AND session=?`,
		tenant, user, session,
	).Scan(&n)
	return n, err
}

// UpsertSummary stores (or replaces) a session summary and records how many
// messages it covered, so callers can detect when it has gone stale.
func (s *Store) UpsertSummary(tenant, user, session, summary string, messageCount int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO session_summaries (tenant, user, session, summary, message_count, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(tenant, user, session) DO UPDATE SET
		   summary=excluded.summary, message_count=excluded.message_count, updated_at=excluded.updated_at`,
		tenant, user, session, summary, messageCount, time.Now().UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("recall: upsert summary: %w", err)
	}
	return nil
}

// Summary returns a session's summary and the message count it covered. A
// missing summary returns ("", 0, nil).
func (s *Store) Summary(tenant, user, session string) (string, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var summary string
	var count int
	err := s.db.QueryRow(
		`SELECT summary, message_count FROM session_summaries WHERE tenant=? AND user=? AND session=?`,
		tenant, user, session,
	).Scan(&summary, &count)
	if err == sql.ErrNoRows {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("recall: get summary: %w", err)
	}
	return summary, count, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// ftsQuery turns an arbitrary user query into a safe FTS5 MATCH expression:
// it extracts word tokens, quotes each (so punctuation can't break syntax),
// and ORs them so any token can match. Returns "" when there are no tokens.
func ftsQuery(q string) string {
	tokens := strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(tokens) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(tokens))
	for _, t := range tokens {
		quoted = append(quoted, `"`+t+`"`)
	}
	return strings.Join(quoted, " OR ")
}
