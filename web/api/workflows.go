package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/workflow"
)

func (s *Server) handleWorkflowRegister(w http.ResponseWriter, r *http.Request) {
	var wf workflow.Workflow
	if err := json.NewDecoder(r.Body).Decode(&wf); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{Source: "api", Target: "workflow", Topic: "workflow.register", Payload: wf})
	if err != nil {
		RespondError(w, 500, "REGISTER_FAILED", err.Error())
		return
	}
	Respond(w, 201, map[string]string{"workflow_id": resp.Payload.(string)})
}

func (s *Server) handleWorkflowList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{Source: "api", Target: "workflow", Topic: "workflow.list"})
	if err != nil {
		RespondError(w, 500, "LIST_FAILED", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleWorkflowRun(w http.ResponseWriter, r *http.Request) {
	wfID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{Source: "api", Target: "workflow", Topic: "workflow.run", Payload: map[string]string{"workflow_id": wfID}})
	if err != nil {
		RespondError(w, 500, "RUN_FAILED", err.Error())
		return
	}
	Respond(w, 201, map[string]string{"run_id": resp.Payload.(string)})
}

func (s *Server) handleWorkflowRunStatus(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{Source: "api", Target: "workflow", Topic: "workflow.status", Payload: runID})
	if err != nil {
		RespondError(w, 500, "STATUS_FAILED", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}
