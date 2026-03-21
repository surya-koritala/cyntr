package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func (s *Server) handleApprovalList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "policy", Topic: "approval.list",
	})
	if err != nil {
		Respond(w, 200, []any{})
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleApprovalApprove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		DecidedBy string `json:"decided_by"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "policy", Topic: "approval.approve",
		Payload: map[string]string{"id": id, "decided_by": body.DecidedBy},
	})
	if err != nil {
		RespondError(w, 400, "APPROVE_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"id": id, "status": "approved", "decided_by": body.DecidedBy})
}

func (s *Server) handleApprovalDeny(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		DecidedBy string `json:"decided_by"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "policy", Topic: "approval.deny",
		Payload: map[string]string{"id": id, "decided_by": body.DecidedBy},
	})
	if err != nil {
		RespondError(w, 400, "DENY_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"id": id, "status": "denied", "decided_by": body.DecidedBy})
}
