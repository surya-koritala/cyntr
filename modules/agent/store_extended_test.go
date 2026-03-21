package agent

import (
	"path/filepath"
	"testing"
)

func TestClearMessages(t *testing.T) {
	store, err := NewSessionStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := AgentConfig{Name: "bot", Tenant: "t"}
	store.SaveSession("sess1", cfg)
	store.AppendMessage("sess1", Message{Role: RoleUser, Content: "hello"})
	store.AppendMessage("sess1", Message{Role: RoleAssistant, Content: "hi"})

	store.ClearMessages("sess1")

	_, msgs, err := store.LoadSession("sess1")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after clear, got %d", len(msgs))
	}
}
