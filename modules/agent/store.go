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

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			tenant TEXT NOT NULL,
			name TEXT NOT NULL,
			config TEXT NOT NULL,
			PRIMARY KEY (tenant, name)
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create agents table: %w", err)
	}

	// Full-text search index for messages
	db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(content)`)

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			tenant TEXT NOT NULL,
			name TEXT NOT NULL,
			email TEXT DEFAULT '',
			role TEXT DEFAULT 'user',
			api_key_hash TEXT DEFAULT '',
			created_at TEXT NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create users table: %w", err)
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

	result, err := s.db.Exec(
		"INSERT INTO messages (session_id, role, content, tool_calls, tool_results) VALUES (?, ?, ?, ?, ?)",
		sessionID, int(msg.Role), msg.Content, string(toolCallsJSON), string(toolResultsJSON),
	)
	if err != nil {
		return err
	}
	// Index in FTS for search
	if msg.Content != "" {
		if rowid, rerr := result.LastInsertId(); rerr == nil {
			s.db.Exec("INSERT INTO messages_fts(rowid, content) VALUES (?, ?)", rowid, msg.Content)
		}
	}
	return nil
}

// ClearMessages removes all messages for a session.
func (s *SessionStore) ClearMessages(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
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

// SaveAgent persists an agent configuration.
func (s *SessionStore) SaveAgent(cfg AgentConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		"INSERT OR REPLACE INTO agents (tenant, name, config) VALUES (?, ?, ?)",
		cfg.Tenant, cfg.Name, string(cfgJSON))
	return err
}

// LoadAgents returns all saved agent configs.
func (s *SessionStore) LoadAgents() ([]AgentConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query("SELECT config FROM agents ORDER BY tenant, name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var agents []AgentConfig
	for rows.Next() {
		var cfgJSON string
		rows.Scan(&cfgJSON)
		var cfg AgentConfig
		json.Unmarshal([]byte(cfgJSON), &cfg)
		agents = append(agents, cfg)
	}
	if agents == nil {
		agents = []AgentConfig{}
	}
	return agents, rows.Err()
}

// DeleteAgent removes a saved agent config.
func (s *SessionStore) DeleteAgent(tenant, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM agents WHERE tenant=? AND name=?", tenant, name)
	return err
}

// SearchResult represents a single full-text search match from the messages table.
type SearchResult struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Role      int    `json:"role"`
}

// SearchMessages performs a full-text search across all message content.
func (s *SessionStore) SearchMessages(query string) ([]SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT m.session_id, m.content, m.role
		FROM messages m
		JOIN messages_fts f ON m.id = f.rowid
		WHERE messages_fts MATCH ?
		ORDER BY m.id DESC
		LIMIT 50
	`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		rows.Scan(&r.SessionID, &r.Content, &r.Role)
		results = append(results, r)
	}
	if results == nil {
		results = []SearchResult{}
	}
	return results, rows.Err()
}

// User represents a user account within a tenant.
type User struct {
	ID        string `json:"id"`
	Tenant    string `json:"tenant"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// CreateUser inserts a new user record.
func (s *SessionStore) CreateUser(user User, apiKeyHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"INSERT INTO users (id, tenant, name, email, role, api_key_hash, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		user.ID, user.Tenant, user.Name, user.Email, user.Role, apiKeyHash, user.CreatedAt,
	)
	return err
}

// ListUsers returns all users belonging to a tenant.
func (s *SessionStore) ListUsers(tenant string) ([]User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query("SELECT id, tenant, name, email, role, created_at FROM users WHERE tenant = ? ORDER BY name", tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		var u User
		rows.Scan(&u.ID, &u.Tenant, &u.Name, &u.Email, &u.Role, &u.CreatedAt)
		users = append(users, u)
	}
	if users == nil {
		users = []User{}
	}
	return users, rows.Err()
}

// GetUserByKeyHash looks up a user by the SHA-256 hash of their API key.
func (s *SessionStore) GetUserByKeyHash(hash string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var u User
	err := s.db.QueryRow("SELECT id, tenant, name, email, role, created_at FROM users WHERE api_key_hash = ?", hash).
		Scan(&u.ID, &u.Tenant, &u.Name, &u.Email, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// DeleteUser removes a user by ID.
func (s *SessionStore) DeleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

// Close closes the database.
func (s *SessionStore) Close() error {
	return s.db.Close()
}
