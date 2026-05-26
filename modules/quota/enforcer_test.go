package quota

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func newTestEnforcer(t *testing.T) (*Enforcer, *Store, func()) {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "quota.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	return NewEnforcer(store), store, func() { store.Close() }
}

func TestCheckTokensPass(t *testing.T) {
	e, _, cleanup := newTestEnforcer(t)
	defer cleanup()

	if err := e.SetConfig(QuotaConfig{Tenant: "acme", TokensPerDay: 1000}); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := e.CheckTokens("acme", 500); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
	e.RecordTokens("acme", 500)
	if err := e.CheckTokens("acme", 400); err != nil {
		t.Fatalf("expected pass after 500/1000, got %v", err)
	}
}

func TestCheckTokensExceeded(t *testing.T) {
	e, _, cleanup := newTestEnforcer(t)
	defer cleanup()

	e.SetConfig(QuotaConfig{Tenant: "acme", TokensPerDay: 100})
	e.RecordTokens("acme", 90)

	err := e.CheckTokens("acme", 20)
	if err == nil {
		t.Fatal("expected ErrQuotaExceeded")
	}
	var qe *ErrQuotaExceeded
	if !errors.As(err, &qe) {
		t.Fatalf("expected *ErrQuotaExceeded, got %T", err)
	}
	if qe.Kind != KindTokens || qe.Limit != 100 {
		t.Fatalf("unexpected error fields: %+v", qe)
	}
	if qe.ResetAt.IsZero() {
		t.Error("ResetAt should be set")
	}
}

func TestCheckRateBucket(t *testing.T) {
	e, _, cleanup := newTestEnforcer(t)
	defer cleanup()

	// 2 requests per minute — easy to exhaust deterministically.
	e.SetConfig(QuotaConfig{Tenant: "acme", RequestsPerMinute: 2})

	if err := e.CheckRate("acme"); err != nil {
		t.Fatalf("first request should pass: %v", err)
	}
	if err := e.CheckRate("acme"); err != nil {
		t.Fatalf("second request should pass: %v", err)
	}
	err := e.CheckRate("acme")
	if err == nil {
		t.Fatal("third request should be rate-limited")
	}
	var qe *ErrQuotaExceeded
	if !errors.As(err, &qe) || qe.Kind != KindRate {
		t.Fatalf("expected rate ErrQuotaExceeded, got %v", err)
	}
}

func TestCheckRateRefillsOverTime(t *testing.T) {
	e, _, cleanup := newTestEnforcer(t)
	defer cleanup()

	// 60 req/min → 1 token/sec.
	e.SetConfig(QuotaConfig{Tenant: "acme", RequestsPerMinute: 60})

	// Force-deplete the bucket by setting lastRefill very recently and tokens=0.
	e.mu.Lock()
	e.buckets["acme"] = &rateBucket{capacity: 60, tokens: 0, lastRefill: time.Now().Add(-2 * time.Second)}
	e.mu.Unlock()

	// After ~2s of refill at 1 token/sec, at least one token should be back.
	if err := e.CheckRate("acme"); err != nil {
		t.Fatalf("expected refill to allow request, got %v", err)
	}
}

func TestAcquireAgentSlotConcurrency(t *testing.T) {
	e, _, cleanup := newTestEnforcer(t)
	defer cleanup()

	e.SetConfig(QuotaConfig{Tenant: "acme", MaxConcurrentAgents: 2})

	r1, err := e.AcquireAgentSlot("acme")
	if err != nil {
		t.Fatalf("slot 1: %v", err)
	}
	r2, err := e.AcquireAgentSlot("acme")
	if err != nil {
		t.Fatalf("slot 2: %v", err)
	}
	if _, err := e.AcquireAgentSlot("acme"); err == nil {
		t.Fatal("slot 3 should be denied")
	}

	r1()
	// Releasing twice should be a no-op.
	r1()

	r3, err := e.AcquireAgentSlot("acme")
	if err != nil {
		t.Fatalf("slot after release: %v", err)
	}
	r2()
	r3()
}

func TestRecordSessionDaily(t *testing.T) {
	e, _, cleanup := newTestEnforcer(t)
	defer cleanup()

	e.SetConfig(QuotaConfig{Tenant: "acme", MaxSessionsPerDay: 3})

	for i := 0; i < 3; i++ {
		if err := e.RecordSession("acme"); err != nil {
			t.Fatalf("session %d should pass: %v", i+1, err)
		}
	}
	err := e.RecordSession("acme")
	if err == nil {
		t.Fatal("4th session should breach the cap")
	}
	var qe *ErrQuotaExceeded
	if !errors.As(err, &qe) || qe.Kind != KindSessions {
		t.Fatalf("expected session ErrQuotaExceeded, got %v", err)
	}
}

func TestConfigGetSetRoundTrip(t *testing.T) {
	e, _, cleanup := newTestEnforcer(t)
	defer cleanup()

	cfg := QuotaConfig{
		Tenant: "acme", TokensPerDay: 1_000_000, RequestsPerMinute: 60,
		MaxConcurrentAgents: 4, MaxSessionsPerDay: 50,
	}
	if err := e.SetConfig(cfg); err != nil {
		t.Fatalf("set: %v", err)
	}

	got := e.GetConfig("acme")
	if got != cfg {
		t.Fatalf("roundtrip mismatch:\nwant %+v\ngot  %+v", cfg, got)
	}

	// Drop the in-memory cache to verify persistence.
	e.mu.Lock()
	delete(e.configs, "acme")
	e.mu.Unlock()

	got = e.GetConfig("acme")
	if got != cfg {
		t.Fatalf("post-cache-drop mismatch:\nwant %+v\ngot  %+v", cfg, got)
	}
}

func TestUnlimitedByDefault(t *testing.T) {
	e, _, cleanup := newTestEnforcer(t)
	defer cleanup()

	// No config set — every check must succeed.
	if err := e.CheckTokens("anon", 1_000_000_000); err != nil {
		t.Fatalf("tokens: %v", err)
	}
	if err := e.CheckRate("anon"); err != nil {
		t.Fatalf("rate: %v", err)
	}
	rel, err := e.AcquireAgentSlot("anon")
	if err != nil {
		t.Fatalf("concurrency: %v", err)
	}
	rel()
	if err := e.RecordSession("anon"); err != nil {
		t.Fatalf("session: %v", err)
	}
}

func TestErrQuotaExceededMessage(t *testing.T) {
	err := &ErrQuotaExceeded{
		Tenant: "acme", Kind: KindTokens, Limit: 100, Current: 120,
		ResetAt: time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
	}
	msg := err.Error()
	if msg == "" {
		t.Fatal("error message must not be empty")
	}
}

func TestConcurrentRateChecks(t *testing.T) {
	e, _, cleanup := newTestEnforcer(t)
	defer cleanup()

	e.SetConfig(QuotaConfig{Tenant: "acme", RequestsPerMinute: 50})

	var wg sync.WaitGroup
	var allowed, denied int
	var mu sync.Mutex
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.CheckRate("acme"); err == nil {
				mu.Lock()
				allowed++
				mu.Unlock()
			} else {
				mu.Lock()
				denied++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed > 50 {
		t.Fatalf("rate limit breached: allowed=%d (cap=50)", allowed)
	}
	if denied == 0 {
		t.Fatalf("expected some denials with 100 parallel calls against a 50/min bucket")
	}
}
