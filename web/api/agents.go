package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func (s *Server) handleAgentCreate(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")

	var body struct {
		Name         string            `json:"name"`
		Model        string            `json:"model"`
		SystemPrompt string            `json:"system_prompt"`
		MaxTurns     int               `json:"max_turns"`
		Tools        []string          `json:"tools"`
		Secrets      map[string]string `json:"secrets"`
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
			Tools:        body.Tools,
			Secrets:      body.Secrets,
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

func (s *Server) handleAgentChatStream(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	agentName := r.PathValue("name")
	message := r.URL.Query().Get("message")

	if message == "" {
		RespondError(w, 400, "INVALID_REQUEST", "message query parameter required")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		RespondError(w, 500, "STREAMING_NOT_SUPPORTED", "server does not support streaming")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// For now, use the non-streaming chat via IPC and send result as a single SSE event.
	// Full streaming integration (bypassing IPC for direct provider access) can be added later.
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent:   agentName,
			Tenant:  tid,
			Message: message,
		},
	})
	if err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	chatResp, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		fmt.Fprintf(w, "event: error\ndata: unexpected response type\n\n")
		flusher.Flush()
		return
	}

	// Send the response as SSE events
	data, _ := json.Marshal(map[string]any{
		"type":    "text",
		"content": chatResp.Content,
	})
	fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(data))
	flusher.Flush()

	// Send done event
	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}
