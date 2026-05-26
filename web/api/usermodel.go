package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/usermodel"
)

// handleUserProfileDistill triggers a manual distillation pass for one
// (tid, uid). Returns the DistillResult unchanged so callers can see
// whether the operation actually ran (Skipped/SkipReason) versus produced
// a new profile.
//
// POST /api/v1/tenants/{tid}/users/{uid}/profile/distill
func (s *Server) handleUserProfileDistill(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	uid := r.PathValue("uid")
	if tid == "" || uid == "" {
		RespondError(w, 400, "INVALID_REQUEST", "tenant and user are required")
		return
	}
	// Distillation can take a few seconds with a real LLM. Allow up to 60s
	// — well under the API server's own request-timeout but enough head-
	// room for cold provider connections.
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "usermodel", Topic: usermodel.TopicDistill,
		Payload: map[string]string{"tenant": tid, "user": uid},
	})
	if err != nil {
		if err == ipc.ErrNoHandler {
			RespondError(w, 503, "UNAVAILABLE", "usermodel module not registered")
			return
		}
		RespondError(w, 500, "DISTILL_FAILED", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

// handleUserProfileGet returns the curated profile for (tid, uid).
//
// GET /api/v1/tenants/{tid}/users/{uid}/profile
func (s *Server) handleUserProfileGet(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	uid := r.PathValue("uid")
	if tid == "" || uid == "" {
		RespondError(w, 400, "INVALID_REQUEST", "tenant and user are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "usermodel", Topic: usermodel.TopicGet,
		Payload: map[string]string{"tenant": tid, "user": uid},
	})
	if err != nil {
		if err == ipc.ErrNoHandler {
			RespondError(w, 503, "UNAVAILABLE", "usermodel module not registered")
			return
		}
		RespondError(w, 500, "GET_FAILED", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

// handleUserProfilePut upserts the curated profile for (tid, uid). Either
// profile_md or preferences_md may be supplied (or both); each is upserted
// independently so callers can update one section without clobbering the
// other.
//
// PUT /api/v1/tenants/{tid}/users/{uid}/profile
// body: {"profile_md": "...", "preferences_md": "..."}
func (s *Server) handleUserProfilePut(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	uid := r.PathValue("uid")
	if tid == "" || uid == "" {
		RespondError(w, 400, "INVALID_REQUEST", "tenant and user are required")
		return
	}
	var body struct {
		ProfileMD     *string `json:"profile_md"`
		PreferencesMD *string `json:"preferences_md"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	if body.ProfileMD == nil && body.PreferencesMD == nil {
		RespondError(w, 400, "INVALID_REQUEST", "at least one of profile_md or preferences_md is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if body.ProfileMD != nil {
		if _, err := s.bus.Request(ctx, ipc.Message{
			Source: "api", Target: "usermodel", Topic: usermodel.TopicUpsertProfile,
			Payload: map[string]string{"tenant": tid, "user": uid, "md": *body.ProfileMD},
		}); err != nil {
			if err == ipc.ErrNoHandler {
				RespondError(w, 503, "UNAVAILABLE", "usermodel module not registered")
				return
			}
			RespondError(w, 500, "UPDATE_FAILED", err.Error())
			return
		}
	}
	if body.PreferencesMD != nil {
		if _, err := s.bus.Request(ctx, ipc.Message{
			Source: "api", Target: "usermodel", Topic: usermodel.TopicUpsertPreferences,
			Payload: map[string]string{"tenant": tid, "user": uid, "md": *body.PreferencesMD},
		}); err != nil {
			if err == ipc.ErrNoHandler {
				RespondError(w, 503, "UNAVAILABLE", "usermodel module not registered")
				return
			}
			RespondError(w, 500, "UPDATE_FAILED", err.Error())
			return
		}
	}

	// Return the fresh profile so callers see what was persisted.
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "usermodel", Topic: usermodel.TopicGet,
		Payload: map[string]string{"tenant": tid, "user": uid},
	})
	if err != nil {
		// Update succeeded but readback failed — still return 200 with a
		// status placeholder so the caller knows the write went through.
		Respond(w, 200, map[string]string{"status": "updated"})
		return
	}
	Respond(w, 200, resp.Payload)
}
