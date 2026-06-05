package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/skill"
)

// handleSkillCandidates lists proposed (not-yet-approved) skills. These come
// from the autonomous skill-creation loop (A2/A1). Optional ?status= filter;
// defaults to pending. This makes the proposal queue operable from the
// dashboard/API instead of IPC-only.
func (s *Server) handleSkillCandidates(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "skill_runtime", Topic: skill.TopicCandidates, Payload: status,
	})
	if err != nil {
		RespondError(w, 500, "SKILL_ERROR", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

// handleSkillCandidateApprove approves a pending candidate, installing it.
func (s *Server) handleSkillCandidateApprove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid candidate id")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if _, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "skill_runtime", Topic: skill.TopicCandidateApprove, Payload: id,
	}); err != nil {
		RespondError(w, 500, "APPROVE_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]any{"status": "approved", "id": id})
}

// handleSkillCandidateReject rejects a pending candidate without installing it.
// Optional JSON body: {"reason": "..."}.
func (s *Server) handleSkillCandidateReject(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid candidate id")
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body) // body optional
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if _, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "skill_runtime", Topic: skill.TopicCandidateReject,
		Payload: skill.RejectRequest{ID: id, Reason: body.Reason},
	}); err != nil {
		RespondError(w, 500, "REJECT_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]any{"status": "rejected", "id": id})
}
