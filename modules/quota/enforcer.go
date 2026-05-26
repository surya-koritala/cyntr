package quota

import (
	"sync"
	"time"
)

// nowFn is overridable in tests.
var nowFn = time.Now

// rateBucket is a simple token-bucket per tenant.
type rateBucket struct {
	mu         sync.Mutex
	tokens     int
	capacity   int
	lastRefill time.Time
}

// allow consumes 1 token from the bucket, refilling at `capacity tokens / minute`.
// Returns true if a token was available; otherwise returns the time at which
// the bucket will next have at least one token.
func (b *rateBucket) allow(now time.Time) (bool, time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.capacity <= 0 {
		return true, time.Time{}
	}

	// Refill: tokens accrue linearly across the minute.
	elapsed := now.Sub(b.lastRefill)
	if elapsed > 0 {
		// `capacity` tokens per minute → tokens/sec = capacity/60.
		// Multiply first to keep integer math accurate even for short windows.
		refill := int(elapsed * time.Duration(b.capacity) / time.Minute)
		if refill > 0 {
			b.tokens += refill
			if b.tokens > b.capacity {
				b.tokens = b.capacity
			}
			b.lastRefill = now
		}
	}

	if b.tokens > 0 {
		b.tokens--
		return true, time.Time{}
	}

	// No tokens — calculate when the next one arrives.
	perToken := time.Minute / time.Duration(b.capacity)
	next := b.lastRefill.Add(perToken)
	return false, next
}

// agentSlots is a simple counting semaphore.
type agentSlots struct {
	mu       sync.Mutex
	cap      int
	inUse    int
}

func (a *agentSlots) acquire() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cap <= 0 {
		// Unlimited.
		a.inUse++
		return true
	}
	if a.inUse >= a.cap {
		return false
	}
	a.inUse++
	return true
}

func (a *agentSlots) release() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.inUse > 0 {
		a.inUse--
	}
}

func (a *agentSlots) current() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.inUse
}

// Enforcer is the runtime quota enforcer.
//
// All methods are safe for concurrent use.
type Enforcer struct {
	store *Store

	mu      sync.RWMutex
	configs map[string]QuotaConfig
	buckets map[string]*rateBucket
	slots   map[string]*agentSlots
}

// NewEnforcer constructs an Enforcer backed by the supplied Store. The Store
// may be nil for in-memory operation (tests).
func NewEnforcer(store *Store) *Enforcer {
	e := &Enforcer{
		store:   store,
		configs: make(map[string]QuotaConfig),
		buckets: make(map[string]*rateBucket),
		slots:   make(map[string]*agentSlots),
	}
	// Best-effort warm-up of configs from disk so token-bucket caps reflect
	// persisted values on restart.
	if store != nil {
		// No bulk loader to keep the API minimal — configs are loaded lazily on
		// first use.
	}
	return e
}

// SetConfig updates the in-memory and persisted quota config for a tenant.
//
// If the rate or concurrency caps change, the relevant runtime structures are
// reset so the new limits take effect immediately.
func (e *Enforcer) SetConfig(cfg QuotaConfig) error {
	e.mu.Lock()
	e.configs[cfg.Tenant] = cfg
	// Reset rate bucket + slots to reflect new caps. We rebuild lazily on next
	// access by deleting the existing entries.
	delete(e.buckets, cfg.Tenant)
	delete(e.slots, cfg.Tenant)
	e.mu.Unlock()

	if e.store != nil {
		return e.store.SetConfig(cfg)
	}
	return nil
}

// GetConfig returns the current quota config for a tenant. If none is set, a
// zero (unlimited) config is returned.
func (e *Enforcer) GetConfig(tenant string) QuotaConfig {
	e.mu.RLock()
	cfg, ok := e.configs[tenant]
	e.mu.RUnlock()
	if ok {
		return cfg
	}

	if e.store != nil {
		if persisted, found, err := e.store.GetConfig(tenant); err == nil && found {
			e.mu.Lock()
			e.configs[tenant] = persisted
			e.mu.Unlock()
			return persisted
		}
	}
	return QuotaConfig{Tenant: tenant}
}

// CheckTokens verifies that consuming `requested` tokens would not push the
// tenant over its TokensPerDay limit. It does not consume the tokens — call
// RecordTokens after the call succeeds.
func (e *Enforcer) CheckTokens(tenant string, requested int64) error {
	cfg := e.GetConfig(tenant)
	if cfg.TokensPerDay <= 0 {
		return nil
	}

	now := nowFn()
	var current int64
	if e.store != nil {
		c, err := e.store.CurrentTokens(tenant, now)
		if err == nil {
			current = c
		}
	}

	if current+requested > cfg.TokensPerDay {
		return &ErrQuotaExceeded{
			Tenant:  tenant,
			Kind:    KindTokens,
			Limit:   cfg.TokensPerDay,
			Current: current,
			ResetAt: endOfUTCDay(now),
		}
	}
	return nil
}

// RecordTokens persists token usage against the tenant's daily counter.
// Errors are best-effort and never returned — quota accounting must not block
// or fail an LLM call.
func (e *Enforcer) RecordTokens(tenant string, used int64) {
	if used <= 0 || e.store == nil {
		return
	}
	_, _ = e.store.AddTokens(tenant, used, nowFn())
}

// CheckRate enforces the per-tenant requests-per-minute cap using a token
// bucket. Returns an *ErrQuotaExceeded when the bucket is empty.
func (e *Enforcer) CheckRate(tenant string) error {
	cfg := e.GetConfig(tenant)
	if cfg.RequestsPerMinute <= 0 {
		return nil
	}

	e.mu.Lock()
	b, ok := e.buckets[tenant]
	if !ok {
		b = &rateBucket{
			capacity:   cfg.RequestsPerMinute,
			tokens:     cfg.RequestsPerMinute,
			lastRefill: nowFn(),
		}
		e.buckets[tenant] = b
	}
	e.mu.Unlock()

	allowed, nextAt := b.allow(nowFn())
	if !allowed {
		return &ErrQuotaExceeded{
			Tenant:  tenant,
			Kind:    KindRate,
			Limit:   int64(cfg.RequestsPerMinute),
			Current: int64(cfg.RequestsPerMinute),
			ResetAt: nextAt,
		}
	}
	return nil
}

// AcquireAgentSlot reserves a concurrent-agent slot for the tenant. The
// returned `release` callback returns the slot when the chat completes; it is
// always safe to call (idempotent after the first call).
func (e *Enforcer) AcquireAgentSlot(tenant string) (func(), error) {
	cfg := e.GetConfig(tenant)
	if cfg.MaxConcurrentAgents <= 0 {
		// Unlimited — return a no-op release so callers can defer unconditionally.
		return func() {}, nil
	}

	e.mu.Lock()
	a, ok := e.slots[tenant]
	if !ok {
		a = &agentSlots{cap: cfg.MaxConcurrentAgents}
		e.slots[tenant] = a
	}
	// If the cap was reduced via SetConfig, the slot struct was wiped; otherwise
	// keep the in-flight count consistent across config edits.
	if a.cap != cfg.MaxConcurrentAgents {
		a.cap = cfg.MaxConcurrentAgents
	}
	e.mu.Unlock()

	if !a.acquire() {
		return nil, &ErrQuotaExceeded{
			Tenant:  tenant,
			Kind:    KindConcurrency,
			Limit:   int64(cfg.MaxConcurrentAgents),
			Current: int64(a.current()),
			ResetAt: nowFn(),
		}
	}

	released := false
	var releaseMu sync.Mutex
	return func() {
		releaseMu.Lock()
		defer releaseMu.Unlock()
		if released {
			return
		}
		released = true
		a.release()
	}, nil
}

// RecordSession increments the tenant's daily session counter and returns an
// *ErrQuotaExceeded if the new value would exceed MaxSessionsPerDay.
//
// To preserve the "the session was created" invariant, the increment is
// recorded BEFORE the check — if the cap is breached, the caller should treat
// the session as denied. This matches Dify/Vellum patterns where the counter
// reflects "attempted" sessions.
func (e *Enforcer) RecordSession(tenant string) error {
	cfg := e.GetConfig(tenant)
	if e.store == nil {
		return nil
	}

	now := nowFn()
	count, err := e.store.IncrementSessions(tenant, now)
	if err != nil {
		// Storage failures must not block production traffic — log via caller.
		return nil
	}
	if cfg.MaxSessionsPerDay <= 0 {
		return nil
	}
	if count > int64(cfg.MaxSessionsPerDay) {
		return &ErrQuotaExceeded{
			Tenant:  tenant,
			Kind:    KindSessions,
			Limit:   int64(cfg.MaxSessionsPerDay),
			Current: count,
			ResetAt: endOfUTCDay(now),
		}
	}
	return nil
}

// CurrentUsage reports current rolling counters for a tenant.
func (e *Enforcer) CurrentUsage(tenant string) Usage {
	now := nowFn()
	u := Usage{Tenant: tenant, WindowResetAt: endOfUTCDay(now)}

	if e.store != nil {
		if v, err := e.store.CurrentTokens(tenant, now); err == nil {
			u.TokensToday = v
		}
		if v, err := e.store.CurrentSessions(tenant, now); err == nil {
			u.SessionsToday = v
		}
	}

	e.mu.RLock()
	if a, ok := e.slots[tenant]; ok {
		u.ActiveAgents = a.current()
	}
	if b, ok := e.buckets[tenant]; ok {
		b.mu.Lock()
		u.RateBucketTokens = b.tokens
		b.mu.Unlock()
	}
	e.mu.RUnlock()
	return u
}

// endOfUTCDay returns midnight UTC of the day after `t`.
func endOfUTCDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
}
