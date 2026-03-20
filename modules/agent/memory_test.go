package agent

import (
	"path/filepath"
	"testing"
)

func TestMemoryStoreSaveAndRecall(t *testing.T) {
	dir := t.TempDir()
	ms, err := NewMemoryStore(filepath.Join(dir, "memory.db"))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer ms.Close()

	ms.Save(Memory{ID: "m1", Agent: "bot", Tenant: "finance", Key: "user_pref", Content: "User prefers concise responses"})
	ms.Save(Memory{ID: "m2", Agent: "bot", Tenant: "finance", Key: "project", Content: "Working on Q4 report"})

	memories, err := ms.Recall("bot", "finance")
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("expected 2, got %d", len(memories))
	}
}

func TestMemoryStoreRecallByKey(t *testing.T) {
	dir := t.TempDir()
	ms, _ := NewMemoryStore(filepath.Join(dir, "memory.db"))
	defer ms.Close()

	ms.Save(Memory{ID: "m1", Agent: "bot", Tenant: "t", Key: "pref", Content: "likes blue"})
	ms.Save(Memory{ID: "m2", Agent: "bot", Tenant: "t", Key: "context", Content: "working on X"})

	memories, _ := ms.RecallByKey("bot", "t", "pref")
	if len(memories) != 1 {
		t.Fatalf("expected 1, got %d", len(memories))
	}
	if memories[0].Content != "likes blue" {
		t.Fatalf("got %q", memories[0].Content)
	}
}

func TestMemoryStoreSearch(t *testing.T) {
	dir := t.TempDir()
	ms, _ := NewMemoryStore(filepath.Join(dir, "memory.db"))
	defer ms.Close()

	ms.Save(Memory{ID: "m1", Agent: "bot", Tenant: "t", Key: "fact", Content: "The project deadline is March 30"})
	ms.Save(Memory{ID: "m2", Agent: "bot", Tenant: "t", Key: "fact", Content: "Budget is $50,000"})

	results, _ := ms.Search("bot", "t", "deadline")
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "m1" {
		t.Fatalf("wrong result")
	}
}

func TestMemoryStoreDelete(t *testing.T) {
	dir := t.TempDir()
	ms, _ := NewMemoryStore(filepath.Join(dir, "memory.db"))
	defer ms.Close()

	ms.Save(Memory{ID: "m1", Agent: "bot", Tenant: "t", Key: "temp", Content: "temporary"})
	ms.Delete("m1")

	memories, _ := ms.Recall("bot", "t")
	if len(memories) != 0 {
		t.Fatalf("expected 0, got %d", len(memories))
	}
}

func TestMemoryStoreUpdate(t *testing.T) {
	dir := t.TempDir()
	ms, _ := NewMemoryStore(filepath.Join(dir, "memory.db"))
	defer ms.Close()

	ms.Save(Memory{ID: "m1", Agent: "bot", Tenant: "t", Key: "pref", Content: "old value"})
	ms.Save(Memory{ID: "m1", Agent: "bot", Tenant: "t", Key: "pref", Content: "new value"})

	memories, _ := ms.Recall("bot", "t")
	if len(memories) != 1 {
		t.Fatalf("expected 1 (upsert), got %d", len(memories))
	}
	if memories[0].Content != "new value" {
		t.Fatalf("expected updated, got %q", memories[0].Content)
	}
}

func TestMemoryStoreTenantIsolation(t *testing.T) {
	dir := t.TempDir()
	ms, _ := NewMemoryStore(filepath.Join(dir, "memory.db"))
	defer ms.Close()

	ms.Save(Memory{ID: "m1", Agent: "bot", Tenant: "finance", Key: "x", Content: "finance data"})
	ms.Save(Memory{ID: "m2", Agent: "bot", Tenant: "marketing", Key: "x", Content: "marketing data"})

	fin, _ := ms.Recall("bot", "finance")
	mkt, _ := ms.Recall("bot", "marketing")
	if len(fin) != 1 || len(mkt) != 1 {
		t.Fatal("isolation broken")
	}
	if fin[0].Content != "finance data" {
		t.Fatal("wrong tenant data")
	}
}

func TestMemoryStorePersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "memory.db")

	ms1, _ := NewMemoryStore(dbPath)
	ms1.Save(Memory{ID: "m1", Agent: "bot", Tenant: "t", Key: "fact", Content: "remember this"})
	ms1.Close()

	ms2, _ := NewMemoryStore(dbPath)
	defer ms2.Close()
	memories, _ := ms2.Recall("bot", "t")
	if len(memories) != 1 || memories[0].Content != "remember this" {
		t.Fatal("memory lost")
	}
}

func TestMemoryStoreEmptyRecall(t *testing.T) {
	dir := t.TempDir()
	ms, _ := NewMemoryStore(filepath.Join(dir, "memory.db"))
	defer ms.Close()
	memories, _ := ms.Recall("nobody", "nowhere")
	if len(memories) != 0 {
		t.Fatal("expected empty")
	}
}

func TestFormatForContext(t *testing.T) {
	memories := []Memory{
		{Key: "pref", Content: "User prefers concise responses"},
		{Key: "project", Content: "Working on Q4 report"},
	}
	result := FormatForContext(memories)
	if result == "" {
		t.Fatal("expected formatted output")
	}
	if !contains(result, "concise") {
		t.Fatal("missing content")
	}
}

func TestFormatForContextEmpty(t *testing.T) {
	if FormatForContext(nil) != "" {
		t.Fatal("expected empty")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}
func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
