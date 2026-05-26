package quota

import (
	"path/filepath"
	"testing"
	"time"
)

func newStoreT(t *testing.T) (*Store, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "quota.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return store, func() { store.Close() }
}

func TestStorePersistsConfig(t *testing.T) {
	store, cleanup := newStoreT(t)
	defer cleanup()

	cfg := QuotaConfig{Tenant: "acme", TokensPerDay: 5000, MaxConcurrentAgents: 3}
	if err := store.SetConfig(cfg); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, ok, err := store.GetConfig("acme")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !ok {
		t.Fatal("expected config to exist")
	}
	if got != cfg {
		t.Fatalf("mismatch: %+v != %+v", got, cfg)
	}

	// Missing tenant returns zero + false.
	_, ok, err = store.GetConfig("ghost")
	if err != nil {
		t.Fatalf("get ghost: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for missing tenant")
	}
}

func TestStoreTokenAccumulation(t *testing.T) {
	store, cleanup := newStoreT(t)
	defer cleanup()

	now := time.Now()
	if _, err := store.AddTokens("acme", 100, now); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := store.AddTokens("acme", 250, now); err != nil {
		t.Fatalf("add: %v", err)
	}
	total, err := store.CurrentTokens("acme", now)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if total != 350 {
		t.Fatalf("expected 350, got %d", total)
	}

	// Next day's bucket should be independent.
	tomorrow := now.Add(24 * time.Hour)
	total, _ = store.CurrentTokens("acme", tomorrow)
	if total != 0 {
		t.Fatalf("expected isolation per day, got %d", total)
	}
}

func TestStoreSessionsIsolatedPerTenant(t *testing.T) {
	store, cleanup := newStoreT(t)
	defer cleanup()

	now := time.Now()
	for i := 0; i < 4; i++ {
		store.IncrementSessions("acme", now)
	}
	store.IncrementSessions("globex", now)

	acme, _ := store.CurrentSessions("acme", now)
	if acme != 4 {
		t.Fatalf("acme sessions = %d, want 4", acme)
	}
	globex, _ := store.CurrentSessions("globex", now)
	if globex != 1 {
		t.Fatalf("globex sessions = %d, want 1", globex)
	}
}
