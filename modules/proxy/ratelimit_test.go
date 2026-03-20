package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(3, 1*time.Second)

	if !rl.Allow("finance") { t.Fatal("expected allow 1") }
	if !rl.Allow("finance") { t.Fatal("expected allow 2") }
	if !rl.Allow("finance") { t.Fatal("expected allow 3") }
	if rl.Allow("finance") { t.Fatal("expected deny 4") }
}

func TestRateLimiterPerTenant(t *testing.T) {
	rl := NewRateLimiter(2, 1*time.Second)

	rl.Allow("finance")
	rl.Allow("finance")

	// finance exhausted, marketing should still work
	if !rl.Allow("marketing") { t.Fatal("marketing should be allowed") }
}

func TestRateLimiterReset(t *testing.T) {
	rl := NewRateLimiter(1, 50*time.Millisecond)

	rl.Allow("finance")
	if rl.Allow("finance") { t.Fatal("should be denied") }

	time.Sleep(60 * time.Millisecond)

	if !rl.Allow("finance") { t.Fatal("should be allowed after reset") }
}

func TestRateLimiterMiddleware(t *testing.T) {
	rl := NewRateLimiter(1, 1*time.Second)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	// First request — allowed
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Cyntr-Tenant", "finance")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 { t.Fatalf("expected 200, got %d", w.Code) }

	// Second request — rate limited
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 429 { t.Fatalf("expected 429, got %d", w.Code) }
}
