// Package quota provides per-tenant token budgets, request rate caps,
// concurrent-agent caps, and session caps for multi-tenant isolation.
//
// All limits default to 0 (unlimited), making the module opt-in: existing
// deployments that do not configure quotas will continue to behave as if no
// limits are in effect.
package quota

import (
	"fmt"
	"time"
)

// QuotaConfig is the per-tenant quota configuration.
//
// A value of 0 for any limit means "unlimited" — no enforcement is performed
// for that dimension.
type QuotaConfig struct {
	Tenant              string `json:"tenant"`
	TokensPerDay        int64  `json:"tokens_per_day"`        // 0 = unlimited
	RequestsPerMinute   int    `json:"requests_per_minute"`   // 0 = unlimited
	MaxConcurrentAgents int    `json:"max_concurrent_agents"` // 0 = unlimited
	MaxSessionsPerDay   int    `json:"max_sessions_per_day"`  // 0 = unlimited
}

// IsUnlimited returns true if every limit in the config is zero (the default).
func (c QuotaConfig) IsUnlimited() bool {
	return c.TokensPerDay == 0 &&
		c.RequestsPerMinute == 0 &&
		c.MaxConcurrentAgents == 0 &&
		c.MaxSessionsPerDay == 0
}

// Quota kinds used in IPC payloads and error messages.
const (
	KindTokens      = "tokens"
	KindRate        = "rate"
	KindConcurrency = "concurrency"
	KindSessions    = "sessions"
)

// ErrQuotaExceeded indicates that a quota check failed.
//
// The error carries the relevant limit, current usage, and the reset time so
// callers can return informative responses to clients.
type ErrQuotaExceeded struct {
	Tenant  string
	Kind    string
	Limit   int64
	Current int64
	ResetAt time.Time
}

// Error implements the error interface.
func (e *ErrQuotaExceeded) Error() string {
	return fmt.Sprintf("quota exceeded for tenant %q kind=%s limit=%d current=%d resets_at=%s",
		e.Tenant, e.Kind, e.Limit, e.Current, e.ResetAt.UTC().Format(time.RFC3339))
}

// CheckRequest is the payload for the quota.check IPC topic.
type CheckRequest struct {
	Tenant string `json:"tenant"`
	Kind   string `json:"kind"`
	Amount int64  `json:"amount"`
}

// CheckResponse is the response payload for quota.check.
type CheckResponse struct {
	Allowed bool      `json:"allowed"`
	ResetAt time.Time `json:"reset_at"`
	Current int64     `json:"current"`
	Limit   int64     `json:"limit"`
	Reason  string    `json:"reason,omitempty"`
}

// RecordRequest is the payload for the quota.record IPC topic.
type RecordRequest struct {
	Tenant string `json:"tenant"`
	Kind   string `json:"kind"`
	Amount int64  `json:"amount"`
}

// SlotResponse is returned from quota.slot.acquire. SlotID is empty when the
// acquire was denied (Allowed == false).
type SlotResponse struct {
	Allowed bool   `json:"allowed"`
	SlotID  string `json:"slot_id,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// Usage summarises the current state of a tenant's quotas.
type Usage struct {
	Tenant            string    `json:"tenant"`
	TokensToday       int64     `json:"tokens_today"`
	SessionsToday     int64     `json:"sessions_today"`
	ActiveAgents      int       `json:"active_agents"`
	RateBucketTokens  int       `json:"rate_bucket_tokens"`
	WindowResetAt     time.Time `json:"window_reset_at"`
}
