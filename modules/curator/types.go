// Package curator implements F3: Skill Curator v0 — the bones of a
// Hermes-style self-improving skill loop. The curator records every
// skill invocation, aggregates per-skill stats, classifies skill
// health, and exposes prune suggestions for skills that have been
// failing for too long. The real DSPy/GEPA refinement loop is left
// to v1; this v0 establishes the observability + IPC surface.
package curator

import "time"

// Invocation is the outcome of a single skill execution. The
// curator stores one of these per call as the source of truth for
// score aggregation and health classification.
type Invocation struct {
	SkillName     string     `json:"skill_name"`
	Tenant        string     `json:"tenant"`
	Agent         string     `json:"agent"`
	Success       bool       `json:"success"`
	Error         string     `json:"error,omitempty"`
	DurationMs    int64      `json:"duration_ms"`
	Timestamp     time.Time  `json:"timestamp"`
	LLMJudgeScore *float64   `json:"llm_judge_score,omitempty"`
}

// Health classification labels — produced by the classifier in
// scores.go from the success rate over the last 20 invocations.
const (
	HealthHealthy          = "healthy"
	HealthShaky            = "shaky"
	HealthFailing          = "failing"
	HealthInsufficientData = "insufficient_data"
)

// SkillScore is the aggregated, per-skill view returned by
// GET /api/v1/curator/scores and the curator.scores IPC topic.
type SkillScore struct {
	SkillName       string    `json:"skill_name"`
	Invocations     int       `json:"invocations"`
	SuccessRate     float64   `json:"success_rate"`
	AvgDurationMs   float64   `json:"avg_duration_ms"`
	Last7dCount     int       `json:"last_7d_count"`
	Last7dRate      float64   `json:"last_7d_rate"`
	Prior7dCount    int       `json:"prior_7d_count"`
	Prior7dRate     float64   `json:"prior_7d_rate"`
	Health          string    `json:"health"`
	LastInvokedAt   time.Time `json:"last_invoked_at,omitempty"`
	FirstSeenFailing time.Time `json:"first_seen_failing,omitempty"`
}

// ScoresFilter is the optional payload for the curator.scores IPC
// topic. Zero-value returns all skills.
type ScoresFilter struct {
	SkillName string `json:"skill_name"`
}

// PruneSuggestion identifies a skill that has been classified as
// failing for longer than the configured threshold (7 days).
type PruneSuggestion struct {
	SkillName        string    `json:"skill_name"`
	FirstSeenFailing time.Time `json:"first_seen_failing"`
	FailingForDays   float64   `json:"failing_for_days"`
	SuccessRate      float64   `json:"success_rate"`
	Invocations      int       `json:"invocations"`
}
