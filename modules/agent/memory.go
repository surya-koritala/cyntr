package agent

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Memory represents a single piece of long-term knowledge.
type Memory struct {
	ID        string
	Agent     string
	Tenant    string
	Key       string    // topic/category: "user_preference", "project_context", etc.
	Content   string    // the actual knowledge
	CreatedAt time.Time
	UpdatedAt time.Time
}

// MemoryStore persists long-term agent memories to SQLite.
type MemoryStore struct {
	mu sync.Mutex
	db *sql.DB
}

// NewMemoryStore opens or creates a memory database.
func NewMemoryStore(dbPath string) (*MemoryStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open memory db: %w", err)
	}

	db.Exec("PRAGMA journal_mode=WAL")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			id TEXT PRIMARY KEY,
			agent TEXT NOT NULL,
			tenant TEXT NOT NULL,
			key TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create table: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_memories_agent_tenant ON memories(agent, tenant)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create index: %w", err)
	}

	return &MemoryStore{db: db}, nil
}

// Save stores or updates a memory. If a memory with the same ID exists, it updates.
func (ms *MemoryStore) Save(m Memory) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	if m.ID == "" {
		m.ID = fmt.Sprintf("mem_%d", time.Now().UnixNano())
	}

	_, err := ms.db.Exec(`
		INSERT INTO memories (id, agent, tenant, key, content, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at
	`, m.ID, m.Agent, m.Tenant, m.Key, m.Content, now, now)
	return err
}

// Recall retrieves all memories for an agent in a tenant.
func (ms *MemoryStore) Recall(agent, tenant string) ([]Memory, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	rows, err := ms.db.Query(
		"SELECT id, agent, tenant, key, content, created_at, updated_at FROM memories WHERE agent=? AND tenant=? ORDER BY updated_at DESC",
		agent, tenant,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		var created, updated string
		if err := rows.Scan(&m.ID, &m.Agent, &m.Tenant, &m.Key, &m.Content, &created, &updated); err != nil {
			return nil, err
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, created)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		memories = append(memories, m)
	}
	if memories == nil {
		memories = []Memory{}
	}
	return memories, rows.Err()
}

// RecallByKey retrieves memories matching a specific key.
func (ms *MemoryStore) RecallByKey(agent, tenant, key string) ([]Memory, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	rows, err := ms.db.Query(
		"SELECT id, agent, tenant, key, content, created_at, updated_at FROM memories WHERE agent=? AND tenant=? AND key=? ORDER BY updated_at DESC",
		agent, tenant, key,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		var created, updated string
		rows.Scan(&m.ID, &m.Agent, &m.Tenant, &m.Key, &m.Content, &created, &updated)
		m.CreatedAt, _ = time.Parse(time.RFC3339, created)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		memories = append(memories, m)
	}
	if memories == nil {
		memories = []Memory{}
	}
	return memories, rows.Err()
}

// Search finds memories containing the query text.
func (ms *MemoryStore) Search(agent, tenant, query string) ([]Memory, error) {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	rows, err := ms.db.Query(
		"SELECT id, agent, tenant, key, content, created_at, updated_at FROM memories WHERE agent=? AND tenant=? AND content LIKE ? ORDER BY updated_at DESC LIMIT 20",
		agent, tenant, "%"+query+"%",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		var created, updated string
		rows.Scan(&m.ID, &m.Agent, &m.Tenant, &m.Key, &m.Content, &created, &updated)
		m.CreatedAt, _ = time.Parse(time.RFC3339, created)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updated)
		memories = append(memories, m)
	}
	if memories == nil {
		memories = []Memory{}
	}
	return memories, rows.Err()
}

// Delete removes a memory by ID.
func (ms *MemoryStore) Delete(id string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	_, err := ms.db.Exec("DELETE FROM memories WHERE id=?", id)
	return err
}

// Close closes the database.
func (ms *MemoryStore) Close() error { return ms.db.Close() }

// FormatForContext formats memories as text suitable for injection into agent context.
func FormatForContext(memories []Memory) string {
	if len(memories) == 0 {
		return ""
	}
	result := "## Long-term Memory\n\n"
	for _, m := range memories {
		result += fmt.Sprintf("- [%s] %s\n", m.Key, m.Content)
	}
	return result
}
