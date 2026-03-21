package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/scheduler"
)

func (s *Server) handleScheduleAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tenant    string `json:"tenant"`
		Agent     string `json:"agent"`
		Interval  string `json:"interval"`
		Message   string `json:"message"`
		Channel   string `json:"channel"`
		ChannelID string `json:"channel_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	dur, err := time.ParseDuration(body.Interval)
	if err != nil {
		RespondError(w, 400, "INVALID_INTERVAL", "invalid interval: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	jobID := "job_" + body.Tenant + "_" + body.Agent
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "scheduler", Topic: "scheduler.add",
		Payload: scheduler.Job{
			ID: jobID, Name: body.Agent + " schedule",
			Tenant: body.Tenant, Agent: body.Agent,
			Message: body.Message, Interval: dur,
			DestChannel: body.Channel, DestChannelID: body.ChannelID,
		},
	})
	if err != nil {
		RespondError(w, 500, "SCHEDULE_FAILED", err.Error())
		return
	}
	Respond(w, 201, map[string]string{"status": "scheduled", "id": jobID, "result": resp.Payload.(string)})
}

func (s *Server) handleScheduleList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{Source: "api", Target: "scheduler", Topic: "scheduler.list"})
	if err != nil {
		RespondError(w, 500, "LIST_FAILED", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleScheduleRemove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err := s.bus.Request(ctx, ipc.Message{Source: "api", Target: "scheduler", Topic: "scheduler.remove", Payload: id})
	if err != nil {
		RespondError(w, 500, "REMOVE_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"status": "removed", "id": id})
}
