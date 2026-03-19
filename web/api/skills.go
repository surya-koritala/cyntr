package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func (s *Server) handleSkillList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "skill_runtime", Topic: "skill.list",
	})
	if err != nil {
		RespondError(w, 500, "SKILL_ERROR", err.Error())
		return
	}

	Respond(w, 200, resp.Payload)
}

func (s *Server) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "skill_runtime", Topic: "skill.install",
		Payload: body.Path,
	})
	if err != nil {
		RespondError(w, 500, "INSTALL_FAILED", err.Error())
		return
	}

	Respond(w, 201, map[string]string{"status": "installed"})
}
