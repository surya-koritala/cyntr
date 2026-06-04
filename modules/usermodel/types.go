// Package usermodel implements a curated per-(tenant,user) profile that
// the agent runtime injects into every chat as additional system context.
//
// Unlike modules/agent.MemoryStore, which is flat and append-only, this
// module stores a small markdown profile + preferences block per user. The
// agent can read it via the user_model_read tool and propose edits via
// user_model_write — mirroring the USER.md / SOUL.md pattern from Hermes.
package usermodel

import "time"

// UserProfile is the curated markdown profile for a single (tenant, user).
type UserProfile struct {
	Tenant         string    `json:"tenant"`
	User           string    `json:"user"`
	ProfileMD      string    `json:"profile_md"`
	PreferencesMD  string    `json:"preferences_md"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// MaxSectionBytes is the per-section size cap (profile_md and preferences_md
// are each capped independently). Kept small so the entire profile fits
// comfortably in every chat's system context without crowding out the user's
// own prompt.
const MaxSectionBytes = 4 * 1024

// IPC topics handled by the usermodel module.
const (
	TopicGet               = "usermodel.get"
	TopicUpsertProfile     = "usermodel.upsert_profile"
	TopicUpsertPreferences = "usermodel.upsert_preferences"
	// TopicDistill triggers a synchronous distill for (tenant, user). Payload
	// is map[string]string{"tenant": ..., "user": ...}. Returns DistillResult.
	TopicDistill = "usermodel.distill"
	// TopicRecordActivity appends an activity summary for (tenant, user).
	// Fire-and-forget (Publish); never returns an error to the caller.
	TopicRecordActivity = "usermodel.record_activity"
	// TopicGetFacts returns the active []Fact for (tenant, user). Payload is
	// map[string]string{"tenant": ..., "user": ...}.
	TopicGetFacts = "usermodel.get_facts"
)

// ActivitySummary is one row from the per-user chat activity log used as
// distiller input. Bodies are kept short — the table is not a session
// archive, just enough signal to summarize what the user's been up to.
type ActivitySummary struct {
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

// TenantUser is a (tenant, user) tuple returned by ListActiveUsers.
type TenantUser struct {
	Tenant         string `json:"tenant"`
	User           string `json:"user"`
	RecentSessions int    `json:"recent_sessions"`
}

// DistillResult summarizes a single distill operation. Returned by the
// usermodel.distill IPC handler and the HTTP trigger endpoint.
type DistillResult struct {
	Tenant            string `json:"tenant"`
	User              string `json:"user"`
	SessionsProcessed int    `json:"sessions_processed"`
	OldSize           int    `json:"old_size"`
	NewSize           int    `json:"new_size"`
	LLMTokens         int    `json:"llm_tokens"`
	Skipped           bool   `json:"skipped"`
	SkipReason        string `json:"skip_reason,omitempty"`
	Error             string `json:"error,omitempty"`
}
