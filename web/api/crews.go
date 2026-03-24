package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/crew"
)

func (s *Server) handleCrewCreate(w http.ResponseWriter, r *http.Request) {
	var body crew.Crew
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	if body.Name == "" || len(body.Members) == 0 {
		RespondError(w, 400, "MISSING_FIELDS", "name and members are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "crew", Topic: "crew.create",
		Payload: body,
	})
	if err != nil {
		RespondError(w, 500, "CREATE_FAILED", err.Error())
		return
	}
	Respond(w, 201, resp.Payload)
}

func (s *Server) handleCrewList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "crew", Topic: "crew.list",
	})
	if err != nil {
		Respond(w, 200, []any{})
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleCrewRun(w http.ResponseWriter, r *http.Request) {
	crewID := r.PathValue("id")
	var body struct {
		Input string `json:"input"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "crew", Topic: "crew.run",
		Payload: map[string]string{"crew_id": crewID, "input": body.Input},
	})
	if err != nil {
		RespondError(w, 500, "RUN_FAILED", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleCrewRunStatus(w http.ResponseWriter, r *http.Request) {
	runID := r.PathValue("run_id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "crew", Topic: "crew.status",
		Payload: runID,
	})
	if err != nil {
		RespondError(w, 404, "NOT_FOUND", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleCrewListRuns(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "crew", Topic: "crew.list_runs",
	})
	if err != nil {
		Respond(w, 200, []any{})
		return
	}
	Respond(w, 200, resp.Payload)
}
