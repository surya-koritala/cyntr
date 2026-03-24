package agent

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRetentionDeletesOldMemories(t *testing.T) {
	memStore, _ := NewMemoryStore(filepath.Join(t.TempDir(), "mem.db"))
	defer memStore.Close()

	// Save a memory with old timestamp
	memStore.Save(Memory{Agent: "bot", Tenant: "t", Key: "old", Content: "old fact"})

	// Force the updated_at to be old
	memStore.mu.Lock()
	memStore.db.Exec("UPDATE memories SET updated_at = '2020-01-01T00:00:00Z'")
	memStore.mu.Unlock()

	policy := RetentionPolicy{MemoryTTL: 24 * time.Hour}
	deleted, err := RunRetention(nil, memStore, nil, policy)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 deleted, got %d", deleted)
	}

	// Verify it's gone
	mems, _ := memStore.Recall("bot", "t")
	if len(mems) != 0 {
		t.Fatalf("expected 0 memories, got %d", len(mems))
	}
}

func TestRetentionDeletesOldUsage(t *testing.T) {
	store, _ := NewUsageStore(filepath.Join(t.TempDir(), "usage.db"))
	defer store.Close()

	store.Record(UsageRecord{Timestamp: time.Now(), Tenant: "t", Agent: "bot", Provider: "claude"})

	// Force old timestamp
	store.mu.Lock()
	store.db.Exec("UPDATE usage SET timestamp = '2020-01-01T00:00:00Z'")
	store.mu.Unlock()

	policy := RetentionPolicy{UsageTTL: 24 * time.Hour}
	deleted, _ := RunRetention(nil, nil, store, policy)
	if deleted != 1 {
		t.Fatalf("expected 1 deleted, got %d", deleted)
	}
}

func TestRetentionNoDeleteWhenRecent(t *testing.T) {
	memStore, _ := NewMemoryStore(filepath.Join(t.TempDir(), "mem.db"))
	defer memStore.Close()

	memStore.Save(Memory{Agent: "bot", Tenant: "t", Key: "recent", Content: "fresh fact"})

	policy := RetentionPolicy{MemoryTTL: 24 * time.Hour}
	deleted, _ := RunRetention(nil, memStore, nil, policy)
	if deleted != 0 {
		t.Fatalf("expected 0 deleted for recent data, got %d", deleted)
	}
}

func TestRetentionZeroTTLSkips(t *testing.T) {
	policy := RetentionPolicy{} // all zero
	deleted, _ := RunRetention(nil, nil, nil, policy)
	if deleted != 0 {
		t.Fatalf("expected 0, got %d", deleted)
	}
}
