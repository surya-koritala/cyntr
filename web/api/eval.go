package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/eval"
)

func (s *Server) handleEvalRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent  string           `json:"agent"`
		Tenant string           `json:"tenant"`
		Cases  []eval.EvalCase  `json:"cases"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	if len(body.Cases) == 0 {
		RespondError(w, 400, "MISSING_CASES", "at least one eval case required")
		return
	}
	// Propagate top-level agent/tenant to cases that don't specify their own
	for i := range body.Cases {
		if body.Cases[i].Agent == "" {
			body.Cases[i].Agent = body.Agent
		}
		if body.Cases[i].Tenant == "" {
			body.Cases[i].Tenant = body.Tenant
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "eval", Topic: "eval.run",
		Payload: body.Cases,
	})
	if err != nil {
		RespondError(w, 500, "EVAL_FAILED", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleEvalStatus(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "eval", Topic: "eval.status",
		Payload: runID,
	})
	if err != nil {
		RespondError(w, 404, "NOT_FOUND", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleEvalList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "eval", Topic: "eval.list",
	})
	if err != nil {
		Respond(w, 200, []any{})
		return
	}
	Respond(w, 200, resp.Payload)
}
