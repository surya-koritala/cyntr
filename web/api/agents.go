package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func (s *Server) handleAgentCreate(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")

	var body struct {
		Name         string `json:"name"`
		Model        string `json:"model"`
		SystemPrompt string `json:"system_prompt"`
		MaxTurns     int    `json:"max_turns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name:         body.Name,
			Tenant:       tid,
			Model:        body.Model,
			SystemPrompt: body.SystemPrompt,
			MaxTurns:     body.MaxTurns,
		},
	})
	if err != nil {
		RespondError(w, 500, "CREATE_FAILED", err.Error())
		return
	}

	Respond(w, 201, map[string]string{"status": "created", "agent": body.Name, "tenant": tid})
}

func (s *Server) handleAgentChat(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	agentName := r.PathValue("name")

	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent:   agentName,
			Tenant:  tid,
			Message: body.Message,
		},
	})
	if err != nil {
		RespondError(w, 500, "CHAT_FAILED", err.Error())
		return
	}

	chatResp, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		RespondError(w, 500, "INTERNAL", "unexpected response type")
		return
	}

	Respond(w, 200, map[string]any{
		"agent":      chatResp.Agent,
		"content":    chatResp.Content,
		"tools_used": chatResp.ToolsUsed,
	})
}
