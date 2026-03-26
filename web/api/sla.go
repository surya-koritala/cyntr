package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/sla"
)

func (s *Server) handleSLAAddRule(w http.ResponseWriter, r *http.Request) {
	var rule sla.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "sla", Topic: "sla.add_rule", Payload: rule,
	})
	if err != nil {
		RespondError(w, 500, "ADD_FAILED", err.Error())
		return
	}
	Respond(w, 201, map[string]string{"status": "created", "id": rule.ID})
}

func (s *Server) handleSLAListRules(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "sla", Topic: "sla.list_rules",
	})
	if err != nil {
		Respond(w, 200, []any{})
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleSLARemoveRule(w http.ResponseWriter, r *http.Request) {
	ruleID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "sla", Topic: "sla.remove_rule", Payload: ruleID,
	})
	if err != nil {
		RespondError(w, 404, "NOT_FOUND", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"status": "removed", "id": ruleID})
}

func (s *Server) handleSLAViolations(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "sla", Topic: "sla.violations",
	})
	if err != nil {
		Respond(w, 200, []any{})
		return
	}
	Respond(w, 200, resp.Payload)
}
