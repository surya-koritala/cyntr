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

// Close closes the database.
func (s *SessionStore) Close() error {
	return s.db.Close()
}
