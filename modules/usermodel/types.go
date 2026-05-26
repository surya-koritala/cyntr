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
	TopicGet                = "usermodel.get"
	TopicUpsertProfile      = "usermodel.upsert_profile"
	TopicUpsertPreferences  = "usermodel.upsert_preferences"
)
