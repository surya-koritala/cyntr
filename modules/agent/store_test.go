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
