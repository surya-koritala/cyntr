package proxy

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter provides per-tenant request rate limiting.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rate     int           // requests per interval
	interval time.Duration
}

type bucket struct {
	tokens    int
	lastReset time.Time
}

// NewRateLimiter creates a rate limiter.
// rate is the max requests per interval per tenant.
func NewRateLimiter(rate int, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets:  make(map[string]*bucket),
		rate:     rate,
		interval: interval,
	}
}

// Allow checks if a request from the given tenant is allowed.
func (rl *RateLimiter) Allow(tenant string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[tenant]
	if !ok {
		b = &bucket{tokens: rl.rate, lastReset: time.Now()}
		rl.buckets[tenant] = b
	}

	// Reset bucket if interval has passed
	if time.Since(b.lastReset) >= rl.interval {
		b.tokens = rl.rate
		b.lastReset = time.Now()
	}

	if b.tokens <= 0 {
		return false
	}

	b.tokens--
	return true
}

// Middleware returns an HTTP middleware that enforces rate limits.
// Reads tenant from X-Cyntr-Tenant header.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant := r.Header.Get("X-Cyntr-Tenant")
		if tenant == "" {
			tenant = "_anonymous"
		}

		if !rl.Allow(tenant) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
