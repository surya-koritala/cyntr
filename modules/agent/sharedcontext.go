package agent

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/log"
	_ "modernc.org/sqlite"
)

var sharedContextLogger = log.Default().WithModule("agent_shared_context")

// SharedContextEntry is a single note written into a coordination channel by a
// coordinator agent and read (read-only) by its worker subagents (#48).
type SharedContextEntry struct {
	Channel   string    `json:"channel"` // orchestration batch id (the fan-out TraceID)
	Tenant    string    `json:"tenant"`
	Key       string    `json:"key"`     // caller-chosen label, e.g. "plan", "schema"
	Content   string    `json:"content"` // the shared value
	Author    string    `json:"author"`  // coordinator agent that wrote it
	CreatedAt time.Time `json:"created_at"`
}

// ContextStore is a lightweight, tenant- and channel-scoped scratchpad that
// backs stateful subagent coordination (#48). A coordinator writes intermediate
// results (a plan, a schema, partial findings) under a channel id; the worker
// subagents it fans out to read that channel read-only. It is built natively on
// SQLite — no external shared-memory dependency — so it preserves Cyntr's
// single-binary and multi-tenant-isolation guarantees: a (tenant, channel) pair
// is the only key, and reads never cross either boundary.
type ContextStore struct {
	mu sync.Mutex
	db *sql.DB
}

// NewContextStore opens or creates the shared-context database.
func NewContextStore(dbPath string) (*ContextStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open shared-context db: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS shared_context (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			channel TEXT NOT NULL,
			tenant TEXT NOT NULL,
			key TEXT NOT NULL,
			content TEXT NOT NULL,
			author TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(tenant, channel, key)
		)
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create shared_context table: %w", err)
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_shared_context_scope ON shared_context(tenant, channel)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create shared_context index: %w", err)
	}
	return &ContextStore{db: db}, nil
}

// Write stores (or overwrites) a note for (tenant, channel, key). Re-writing the
// same key replaces it so a coordinator can update a value in place.
func (cs *ContextStore) Write(e SharedContextEntry) error {
	if e.Tenant == "" || e.Channel == "" || e.Key == "" {
		return fmt.Errorf("shared context: tenant, channel and key are required")
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	now := time.Now().UTC()
	// Orchestrations are short-lived; prune notes older than the TTL on write so
	// the scratchpad stays bounded without a background sweeper or external infra.
	cs.db.Exec("DELETE FROM shared_context WHERE created_at < ?", now.Add(-sharedContextTTL).Format(time.RFC3339))
	_, err := cs.db.Exec(`
		INSERT INTO shared_context (channel, tenant, key, content, author, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant, channel, key)
		DO UPDATE SET content=excluded.content, author=excluded.author, created_at=excluded.created_at
	`, e.Channel, e.Tenant, e.Key, e.Content, e.Author, now.Format(time.RFC3339))
	return err
}

// sharedContextTTL bounds how long a coordination note lives. Orchestrations
// complete in seconds-to-minutes, so a day is generously past any live batch.
const sharedContextTTL = 24 * time.Hour

// Read returns every note in (tenant, channel), most recent first. The tenant
// and channel are both required: there is no way to read across either, so a
// worker can only ever see its own batch's channel in its own tenant.
func (cs *ContextStore) Read(tenant, channel string) ([]SharedContextEntry, error) {
	if tenant == "" || channel == "" {
		return []SharedContextEntry{}, nil
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	rows, err := cs.db.Query(
		"SELECT channel, tenant, key, content, author, created_at FROM shared_context WHERE tenant=? AND channel=? ORDER BY created_at DESC, id DESC",
		tenant, channel,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []SharedContextEntry{}
	for rows.Next() {
		var e SharedContextEntry
		var created string
		if err := rows.Scan(&e.Channel, &e.Tenant, &e.Key, &e.Content, &e.Author, &created); err != nil {
			return nil, err
		}
		var perr error
		if e.CreatedAt, perr = time.Parse(time.RFC3339, created); perr != nil {
			sharedContextLogger.Warn("corrupt timestamp in shared context", map[string]any{"channel": e.Channel, "value": created})
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Clear removes every note in a channel. Called when an orchestration finishes
// so the scratchpad does not accumulate stale batches.
func (cs *ContextStore) Clear(tenant, channel string) error {
	if tenant == "" || channel == "" {
		return nil
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	_, err := cs.db.Exec("DELETE FROM shared_context WHERE tenant=? AND channel=?", tenant, channel)
	return err
}

// Close closes the database.
func (cs *ContextStore) Close() error { return cs.db.Close() }

// FormatSharedContext renders channel notes for injection into a worker's tool
// result.
func FormatSharedContext(entries []SharedContextEntry) string {
	if len(entries) == 0 {
		return "No shared context has been set for this task."
	}
	out := "## Shared context from the coordinator\n\n"
	for _, e := range entries {
		out += fmt.Sprintf("### %s (set by %s)\n%s\n\n", e.Key, e.Author, e.Content)
	}
	return out
}
