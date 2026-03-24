package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func (s *Server) handleWebhookTrigger(w http.ResponseWriter, r *http.Request) {
	wfID := r.PathValue("workflow_id")

	// Read the webhook payload and store it for the workflow
	body, _ := io.ReadAll(r.Body)
	var payload map[string]any
	json.Unmarshal(body, &payload)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "webhook", Target: "workflow", Topic: "workflow.run",
		Payload: map[string]string{"workflow_id": wfID},
	})
	if err != nil {
		RespondError(w, 500, "TRIGGER_FAILED", err.Error())
		return
	}

	Respond(w, 200, map[string]any{
		"status":      "triggered",
		"workflow_id": wfID,
		"run_id":      resp.Payload,
		"payload":     payload,
	})
}

func (s *Server) handleWebhookAgent(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tenant")
	agentName := r.PathValue("agent")

	body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
	if err != nil {
		RespondError(w, 400, "READ_ERROR", err.Error())
		return
	}

	// Try to extract message from JSON body
	var payload struct {
		Message string `json:"message"`
		Text    string `json:"text"`
		Content string `json:"content"`
	}
	json.Unmarshal(body, &payload)

	message := payload.Message
	if message == "" {
		message = payload.Text
	}
	if message == "" {
		message = payload.Content
	}
	if message == "" {
		message = string(body)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "webhook", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent:   agentName,
			Tenant:  tenant,
			User:    "webhook:" + r.RemoteAddr,
			Message: message,
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
