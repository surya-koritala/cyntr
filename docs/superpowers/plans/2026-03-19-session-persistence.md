# Session Persistence Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist agent conversation sessions to SQLite so they survive restarts. Sessions are stored per-tenant in separate database files.

**Architecture:** A `SessionStore` backed by SQLite sits behind the existing `Session` type. When `AddMessage` is called, it writes to both in-memory history and SQLite. On startup, the runtime loads existing sessions from disk. The store is per-tenant (`~/.cyntr/data/<tenant>/sessions.db`), matching the spec's data storage table.

**Tech Stack:** `modernc.org/sqlite` (already a dep from audit logger).

**Spec:** `docs/superpowers/specs/2026-03-19-cyntr-enterprise-platform-design.md` (Data Storage table)

---

## File Structure

```
modules/agent/
├── store.go               # NEW: SessionStore — SQLite-backed persistence
├── store_test.go          # NEW: Tests with real SQLite
├── session.go             # MODIFY: Wire to store on AddMessage
├── runtime.go             # MODIFY: Accept store, load sessions on start
```

---

### Task 1: Implement SessionStore

**Files:**
- Create: `modules/agent/store.go`
- Create: `modules/agent/store_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/agent/store_test.go`:
```go
package agent

import (
	"path/filepath"
	"testing"
)

func TestStoreCreateAndLoadSession(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSessionStore(filepath.Join(dir, "sessions.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	cfg := AgentConfig{Name: "assistant", Tenant: "finance", Model: "mock", SystemPrompt: "Be helpful.", MaxTurns: 10}

	// Save a session
	if err := store.SaveSession("sess_001", cfg); err != nil {
		t.Fatalf("save session: %v", err)
	}

	// Add messages
	if err := store.AppendMessage("sess_001", Message{Role: RoleUser, Content: "Hello"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := store.AppendMessage("sess_001", Message{Role: RoleAssistant, Content: "Hi there!"}); err != nil {
		t.Fatalf("append: %v", err)
	}

	// Load back
	cfg2, messages, err := store.LoadSession("sess_001")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg2.Name != "assistant" {
		t.Fatalf("expected assistant, got %q", cfg2.Name)
	}
	if cfg2.SystemPrompt != "Be helpful." {
		t.Fatalf("expected system prompt, got %q", cfg2.SystemPrompt)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].Content != "Hello" {
		t.Fatalf("expected Hello, got %q", messages[0].Content)
	}
	if messages[1].Role != RoleAssistant {
		t.Fatalf("expected assistant, got %s", messages[1].Role)
	}
}

func TestStoreListSessions(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewSessionStore(filepath.Join(dir, "sessions.db"))
	defer store.Close()

	store.SaveSession("sess_001", AgentConfig{Name: "a", Tenant: "finance", Model: "mock"})
	store.SaveSession("sess_002", AgentConfig{Name: "b", Tenant: "finance", Model: "mock"})

	sessions, err := store.ListSessions()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestStoreDeleteSession(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewSessionStore(filepath.Join(dir, "sessions.db"))
	defer store.Close()

	store.SaveSession("sess_001", AgentConfig{Name: "a", Tenant: "t", Model: "mock"})
	store.AppendMessage("sess_001", Message{Role: RoleUser, Content: "Hi"})

	if err := store.DeleteSession("sess_001"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	sessions, _ := store.ListSessions()
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions after delete, got %d", len(sessions))
	}
}

func TestStoreLoadNonexistent(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewSessionStore(filepath.Join(dir, "sessions.db"))
	defer store.Close()

	_, _, err := store.LoadSession("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestStorePersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sessions.db")

	// Write with first store instance
	store1, _ := NewSessionStore(dbPath)
	store1.SaveSession("sess_001", AgentConfig{Name: "bot", Tenant: "t", Model: "mock"})
	store1.AppendMessage("sess_001", Message{Role: RoleUser, Content: "Remember this"})
	store1.Close()

	// Read with second store instance (simulates restart)
	store2, _ := NewSessionStore(dbPath)
	defer store2.Close()

	_, messages, err := store2.LoadSession("sess_001")
	if err != nil {
		t.Fatalf("load after reopen: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message after reopen, got %d", len(messages))
	}
	if messages[0].Content != "Remember this" {
		t.Fatalf("expected 'Remember this', got %q", messages[0].Content)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/ -run TestStore -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement SessionStore**

Create `modules/agent/store.go`:
```go
package agent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// SessionStore persists agent sessions to SQLite.
type SessionStore struct {
	mu sync.Mutex
	db *sql.DB
}

// NewSessionStore opens (or creates) a SQLite database for session storage.
func NewSessionStore(dbPath string) (*SessionStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open session db: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			config TEXT NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create sessions table: %w", err)
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			role INTEGER NOT NULL,
			content TEXT NOT NULL,
			tool_calls TEXT,
			tool_results TEXT,
			FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create messages table: %w", err)
	}

	// Enable foreign keys for CASCADE delete
	db.Exec("PRAGMA foreign_keys=ON")

	return &SessionStore{db: db}, nil
}

// SaveSession creates or updates a session record.
func (s *SessionStore) SaveSession(id string, cfg AgentConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	_, err = s.db.Exec(
		"INSERT OR REPLACE INTO sessions (id, config) VALUES (?, ?)",
		id, string(cfgJSON),
	)
	return err
}

// AppendMessage adds a message to a session's history.
func (s *SessionStore) AppendMessage(sessionID string, msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	toolCallsJSON, _ := json.Marshal(msg.ToolCalls)
	toolResultsJSON, _ := json.Marshal(msg.ToolResults)

	_, err := s.db.Exec(
		"INSERT INTO messages (session_id, role, content, tool_calls, tool_results) VALUES (?, ?, ?, ?, ?)",
		sessionID, int(msg.Role), msg.Content, string(toolCallsJSON), string(toolResultsJSON),
	)
	return err
}

// LoadSession returns the config and message history for a session.
func (s *SessionStore) LoadSession(id string) (AgentConfig, []Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load config
	var cfgJSON string
	err := s.db.QueryRow("SELECT config FROM sessions WHERE id = ?", id).Scan(&cfgJSON)
	if err == sql.ErrNoRows {
		return AgentConfig{}, nil, fmt.Errorf("session %q not found", id)
	}
	if err != nil {
		return AgentConfig{}, nil, err
	}

	var cfg AgentConfig
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return AgentConfig{}, nil, fmt.Errorf("unmarshal config: %w", err)
	}

	// Load messages in order
	rows, err := s.db.Query(
		"SELECT role, content, tool_calls, tool_results FROM messages WHERE session_id = ? ORDER BY id ASC", id,
	)
	if err != nil {
		return AgentConfig{}, nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var roleInt int
		var content, toolCallsStr, toolResultsStr string
		if err := rows.Scan(&roleInt, &content, &toolCallsStr, &toolResultsStr); err != nil {
			return AgentConfig{}, nil, err
		}

		msg := Message{
			Role:    Role(roleInt),
			Content: content,
		}

		if toolCallsStr != "" && toolCallsStr != "null" {
			json.Unmarshal([]byte(toolCallsStr), &msg.ToolCalls)
		}
		if toolResultsStr != "" && toolResultsStr != "null" {
			json.Unmarshal([]byte(toolResultsStr), &msg.ToolResults)
		}

		messages = append(messages, msg)
	}

	return cfg, messages, rows.Err()
}

// ListSessions returns all session IDs.
func (s *SessionStore) ListSessions() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query("SELECT id FROM sessions ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	if ids == nil {
		ids = []string{}
	}
	return ids, rows.Err()
}

// DeleteSession removes a session and its messages.
func (s *SessionStore) DeleteSession(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Delete messages first (foreign key may not cascade in all SQLite builds)
	s.db.Exec("DELETE FROM messages WHERE session_id = ?", id)
	_, err := s.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	return err
}

// Close closes the database.
func (s *SessionStore) Close() error {
	return s.db.Close()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/ -run TestStore -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/agent/store.go modules/agent/store_test.go
git commit -m "feat(agent): implement SQLite-backed SessionStore for persistent conversations"
```

---

### Task 2: Wire Store into Session and Runtime

**Files:**
- Modify: `modules/agent/session.go`
- Modify: `modules/agent/runtime.go`

- [ ] **Step 1: Update Session to optionally persist via store**

Add to `modules/agent/session.go` — a `SetStore` method and persist on `AddMessage`:

Add a `store` field and `SetStore` method:
```go
type Session struct {
	mu      sync.RWMutex
	id      string
	config  AgentConfig
	history []Message
	store   *SessionStore
}

func (s *Session) SetStore(store *SessionStore) {
	s.store = store
}
```

Update `AddMessage` to persist:
```go
func (s *Session) AddMessage(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, msg)
	if s.store != nil {
		s.store.AppendMessage(s.id, msg)
	}
}
```

- [ ] **Step 2: Update Runtime to accept and use a SessionStore**

Add to `modules/agent/runtime.go`:

Add `store` field to Runtime and a setter:
```go
type Runtime struct {
	// ... existing fields
	store     *SessionStore
}

func (r *Runtime) SetSessionStore(store *SessionStore) {
	r.store = store
}
```

In `handleCreate`, after creating the session, save to store and wire it:
```go
// In handleCreate, after creating the session:
session := NewSession(sessID, cfg)
if r.store != nil {
	r.store.SaveSession(sessID, cfg)
	session.SetStore(r.store)
}
```

- [ ] **Step 3: Run all tests**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/... -v -count=1 -race`
Expected: All PASS (existing tests work — store is optional/nil)

- [ ] **Step 4: Run full suite**

Run: `cd /Users/suryakoritala/Cyntr && go test ./... -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/agent/session.go modules/agent/runtime.go
git commit -m "feat(agent): wire SessionStore into Session and Runtime for persistent conversations"
```

---

### Task 3: Update CLI + Final Verification

- [ ] **Step 1: Update cmd/cyntr/main.go to create session store**

In `runStart()`, before creating agentRuntime, add:
```go
	sessionStore, err := agent.NewSessionStore("sessions.db")
	if err != nil {
		fmt.Fprintf(os.Stderr, "session store error: %v\n", err)
		os.Exit(1)
	}
```

After creating agentRuntime:
```go
	agentRuntime.SetSessionStore(sessionStore)
```

- [ ] **Step 2: Build and verify**

Run: `cd /Users/suryakoritala/Cyntr && go build -o cyntr ./cmd/cyntr && ./cyntr version`

- [ ] **Step 3: Run full test suite**

Run: `cd /Users/suryakoritala/Cyntr && go test ./... -count=1 -race && go vet ./...`

- [ ] **Step 4: Commit and push**

```bash
git add cmd/cyntr/main.go
git commit -m "feat(cli): enable SQLite session persistence for agent conversations"
git push
```
