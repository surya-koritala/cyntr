package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

func (s *Server) handlePolicyRulesList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "policy", Topic: "policy.list",
	})
	if err != nil {
		RespondError(w, 500, "POLICY_ERROR", err.Error())
		return
	}

	Respond(w, 200, resp.Payload)
}

func (s *Server) handlePolicyTest(w http.ResponseWriter, r *http.Request) {
	var body policy.CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "policy", Topic: "policy.check",
		Payload: body,
	})
	if err != nil {
		RespondError(w, 500, "POLICY_ERROR", err.Error())
		return
	}

	Respond(w, 200, resp.Payload)
}
