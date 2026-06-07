package proxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxRateLimitBuckets caps the number of distinct buckets retained at any time
// as a hard backstop against unbounded map growth, even if eviction lags.
const maxRateLimitBuckets = 100_000

// RateLimiter provides per-tenant request rate limiting.
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	rate     int // requests per interval
	interval time.Duration

	// identitySecret, when set, makes the middleware key buckets on the
	// authenticated (HMAC-signed) caller identity instead of the raw,
	// attacker-controlled X-Cyntr-Tenant header.
	identitySecret string

	// lastSweep tracks when stale buckets were last evicted so the sweep runs
	// at most once per interval.
	lastSweep time.Time
}

type bucket struct {
	tokens    int
	lastReset time.Time
	lastSeen  time.Time
}

// NewRateLimiter creates a rate limiter.
// rate is the max requests per interval per tenant.
func NewRateLimiter(rate int, interval time.Duration) *RateLimiter {
	return &RateLimiter{
		buckets:   make(map[string]*bucket),
		rate:      rate,
		interval:  interval,
		lastSweep: time.Now(),
	}
}

// SetIdentitySecret configures the shared secret used to authenticate the
// caller-supplied identity for rate-limit keying. See identitySecret.
func (rl *RateLimiter) SetIdentitySecret(secret string) {
	rl.identitySecret = secret
}

// Allow checks if a request from the given tenant is allowed.
func (rl *RateLimiter) Allow(tenant string) bool {
	now := time.Now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Evict stale buckets so the map cannot grow without bound when keyed by
	// many distinct (potentially attacker-supplied) tenants. A bucket is stale
	// once it has been idle for more than two reset intervals. The sweep runs
	// at most once per interval to keep Allow O(1) amortised.
	rl.evictStaleLocked(now)

	b, ok := rl.buckets[tenant]
	if !ok {
		// Hard backstop: if the map is somehow at capacity, deny rather than
		// allow unbounded allocation.
		if len(rl.buckets) >= maxRateLimitBuckets {
			return false
		}
		b = &bucket{tokens: rl.rate, lastReset: now}
		rl.buckets[tenant] = b
	}
	b.lastSeen = now

	// Reset bucket if interval has passed
	if now.Sub(b.lastReset) >= rl.interval {
		b.tokens = rl.rate
		b.lastReset = now
	}

	if b.tokens <= 0 {
		return false
	}

	b.tokens--
	return true
}

// evictStaleLocked removes buckets that have been idle longer than the eviction
// TTL. Callers must hold rl.mu.
func (rl *RateLimiter) evictStaleLocked(now time.Time) {
	if now.Sub(rl.lastSweep) < rl.interval {
		return
	}
	rl.lastSweep = now

	ttl := 2 * rl.interval
	for k, b := range rl.buckets {
		if now.Sub(b.lastSeen) > ttl {
			delete(rl.buckets, k)
		}
	}
}

// rateLimitKey derives the bucket key. When an identity secret is configured it
// uses the HMAC-signed X-Cyntr-Identity credential (verified in constant time)
// so the key cannot be forged via raw headers; otherwise it falls back to the
// X-Cyntr-Tenant header (trusted-proxy / development mode).
func (rl *RateLimiter) rateLimitKey(r *http.Request) string {
	if rl.identitySecret != "" {
		identity := r.Header.Get("X-Cyntr-Identity")
		sig := r.Header.Get("X-Cyntr-Identity-Sig")
		if identity != "" && sig != "" {
			mac := hmac.New(sha256.New, []byte(rl.identitySecret))
			mac.Write([]byte(identity))
			expected := hex.EncodeToString(mac.Sum(nil))
			if hmac.Equal([]byte(sig), []byte(expected)) {
				tenant, _, _ := strings.Cut(identity, ":")
				if tenant != "" {
					return tenant
				}
			}
		}
		// Unauthenticated callers share a single bucket so they cannot expand
		// the map or evade limiting by rotating forged tenant values.
		return "_unauthenticated"
	}

	tenant := r.Header.Get("X-Cyntr-Tenant")
	if tenant == "" {
		tenant = "_anonymous"
	}
	return tenant
}

// Middleware returns an HTTP middleware that enforces rate limits.
// The bucket key is the authenticated caller identity (see rateLimitKey).
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.Allow(rl.rateLimitKey(r)) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
