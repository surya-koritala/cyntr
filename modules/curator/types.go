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

// InvocationContext is the payload the judge consumes when scoring
// a single skill invocation. It carries the full request / response
// pair plus the metadata the curator already knows (success, error,
// tools used) so the LLM has enough signal to grade meaningfully.
type InvocationContext struct {
	SkillName     string   `json:"skill_name"`
	UserMessage   string   `json:"user_message"`
	AgentResponse string   `json:"agent_response"`
	ToolsUsed     []string `json:"tools_used"`
	Success       bool     `json:"success"`
	Error         string   `json:"error,omitempty"`
	// InvocationID, if set, lets the judge write its score back to
	// the matching invocations row. Unset = judge result is returned
	// to the caller only (no persistence).
	InvocationID int64 `json:"invocation_id,omitempty"`
}

// JudgeVerdict labels — coarse-grained buckets so dashboards and
// alerting can group judgments without staring at raw scores.
const (
	VerdictGood       = "good"
	VerdictAcceptable = "acceptable"
	VerdictPoor       = "poor"
)

// JudgeResult is what an LLM judge returns for a single invocation.
// Score is 0-1, Verdict is one of {good, acceptable, poor}.
type JudgeResult struct {
	Score   float64 `json:"score"`
	Reason  string  `json:"reason"`
	Verdict string  `json:"verdict"`
}

// PruneReport is the outcome of a single auto-prune pass. One entry
// per skill that the prune logic considered. Disabled=true means we
// actually flipped the skill off; reasons + samples explain why.
type PruneReport struct {
	RanAt   time.Time           `json:"ran_at"`
	Entries []PruneReportEntry  `json:"entries"`
}

// PruneReportEntry is one skill in a PruneReport.
type PruneReportEntry struct {
	Skill    string   `json:"skill"`
	Disabled bool     `json:"disabled"`
	Reason   string   `json:"reason"`
	Samples  []string `json:"samples"`
}

// ConsolidationReport surfaces pairs of skills that overlap heavily
// on their declared tool surface — candidates the operator might
// want to merge. v1 just reports; v2 might auto-suggest a merge.
type ConsolidationReport struct {
	GeneratedAt time.Time                 `json:"generated_at"`
	Suggestions []ConsolidationSuggestion `json:"suggestions"`
}

// ConsolidationSuggestion is a single pair of overlapping skills.
type ConsolidationSuggestion struct {
	SkillA       string   `json:"skill_a"`
	SkillB       string   `json:"skill_b"`
	SharedTools  []string `json:"shared_tools"`
	Jaccard      float64  `json:"jaccard"`
	InvocationsA int      `json:"invocations_a"`
	InvocationsB int      `json:"invocations_b"`
	Note         string   `json:"note"`
}

// SkillDisabledEvent is the IPC payload published when the auto-prune
// loop disables a skill. Subscribers (notify, audit, dashboards) use
// it to alert / log the action.
type SkillDisabledEvent struct {
	Skill   string    `json:"skill"`
	Reason  string    `json:"reason"`
	Samples []string  `json:"samples"`
	At      time.Time `json:"at"`
}
