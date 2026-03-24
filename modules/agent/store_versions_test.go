package agent

import (
	"path/filepath"
	"testing"
)

func TestSaveAndListAgentVersions(t *testing.T) {
	store, _ := NewSessionStore(filepath.Join(t.TempDir(), "test.db"))
	defer store.Close()

	cfg := AgentConfig{Name: "bot", Tenant: "demo", Model: "claude", SystemPrompt: "v1"}
	store.SaveAgentVersion(cfg)

	cfg.SystemPrompt = "v2"
	store.SaveAgentVersion(cfg)

	versions, err := store.ListAgentVersions("demo", "bot")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
}

func TestGetAgentVersion(t *testing.T) {
	store, _ := NewSessionStore(filepath.Join(t.TempDir(), "test.db"))
	defer store.Close()

	cfg := AgentConfig{Name: "bot", Tenant: "demo", Model: "claude", SystemPrompt: "original"}
	store.SaveAgentVersion(cfg)

	restored, err := store.GetAgentVersion("demo", "bot", 1)
	if err != nil {
		t.Fatal(err)
	}
	if restored.SystemPrompt != "original" {
		t.Fatalf("expected 'original', got %q", restored.SystemPrompt)
	}
}

func TestGetAgentVersionNotFound(t *testing.T) {
	store, _ := NewSessionStore(filepath.Join(t.TempDir(), "test.db"))
	defer store.Close()

	_, err := store.GetAgentVersion("demo", "bot", 999)
	if err == nil {
		t.Fatal("expected error for non-existent version")
	}
}

func TestVersionsEmpty(t *testing.T) {
	store, _ := NewSessionStore(filepath.Join(t.TempDir(), "test.db"))
	defer store.Close()

	versions, _ := store.ListAgentVersions("demo", "nonexistent")
	if len(versions) != 0 {
		t.Fatalf("expected 0 versions, got %d", len(versions))
	}
}
