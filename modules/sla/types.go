package sla

import "time"

// Rule defines an SLA threshold for an agent.
type Rule struct {
	ID             string   `json:"id"`
	Agent          string   `json:"agent"`            // agent name or "*" for all
	Tenant         string   `json:"tenant"`           // tenant or "*"
	MaxResponseMs  int64    `json:"max_response_ms"`  // max allowed avg response time
	MaxErrorRate   float64  `json:"max_error_rate"`   // max error percentage (0-100)
	WindowMinutes  int      `json:"window_minutes"`   // evaluation window (default 5)
	NotifyChannels []string `json:"notify_channels"`  // channels to alert on breach
	Enabled        bool     `json:"enabled"`
}

// Violation records when an SLA was breached.
type Violation struct {
	ID         string    `json:"id"`
	RuleID     string    `json:"rule_id"`
	Agent      string    `json:"agent"`
	Tenant     string    `json:"tenant"`
	Type       string    `json:"type"`      // "latency" or "error_rate"
	Value      float64   `json:"value"`     // actual value
	Threshold  float64   `json:"threshold"` // SLA threshold
	Timestamp  time.Time `json:"timestamp"`
	Resolved   bool      `json:"resolved"`
	ResolvedAt time.Time `json:"resolved_at,omitempty"`
}

// latencyRecord tracks a single response time measurement.
type latencyRecord struct {
	DurationMs int64
	Timestamp  time.Time
}
