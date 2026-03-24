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

func (s *Server) handleAgentList(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.list",
		Payload: tid,
	})
	if err != nil {
		// If agent.list not implemented, return empty list
		Respond(w, 200, []any{})
		return
	}

	Respond(w, 200, resp.Payload)
}

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

	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second) // 5 min for multi-tool chains
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent:   agentName,
			Tenant:  tid,
			Message: body.Message,
		},
		TraceID: traceID(r),
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

func (s *Server) handleAgentGet(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.get",
		Payload: map[string]string{"tenant": tid, "name": name},
	})
	if err != nil {
		RespondError(w, 404, "NOT_FOUND", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleAgentDelete(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.delete",
		Payload: map[string]string{"tenant": tid, "name": name},
	})
	if err != nil {
		RespondError(w, 500, "DELETE_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"status": "deleted", "agent": name, "tenant": tid})
}

func (s *Server) handleAgentSessions(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.sessions",
		Payload: map[string]string{"tenant": tid, "name": name},
	})
	if err != nil {
		RespondError(w, 500, "SESSION_ERROR", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleSessionMessages(w http.ResponseWriter, r *http.Request) {
	sid := r.PathValue("sid")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.session.messages",
		Payload: sid,
	})
	if err != nil {
		RespondError(w, 500, "MESSAGE_ERROR", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleAgentMemories(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.memories",
		Payload: map[string]string{"tenant": tid, "name": name},
	})
	if err != nil {
		RespondError(w, 500, "MEMORY_ERROR", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	mid := r.PathValue("mid")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.memory.delete",
		Payload: mid,
	})
	if err != nil {
		RespondError(w, 500, "DELETE_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"status": "deleted", "id": mid})
}

func (s *Server) handleAgentUpdate(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	name := r.PathValue("name")

	var body struct {
		Model        string   `json:"model"`
		SystemPrompt string   `json:"system_prompt"`
		MaxTurns     int      `json:"max_turns"`
		Tools        []string `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.update",
		Payload: agent.AgentConfig{
			Name:         name,
			Tenant:       tid,
			Model:        body.Model,
			SystemPrompt: body.SystemPrompt,
			MaxTurns:     body.MaxTurns,
			Tools:        body.Tools,
		},
	})
	if err != nil {
		RespondError(w, 500, "UPDATE_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"status": "updated", "agent": name, "tenant": tid})
}

func (s *Server) handleAgentSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		RespondError(w, 400, "MISSING_QUERY", "q parameter is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.search",
		Payload: query,
	})
	if err != nil {
		RespondError(w, 500, "SEARCH_ERROR", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleAgentVersions(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.versions",
		Payload: map[string]string{"tenant": tid, "name": name},
	})
	if err != nil {
		Respond(w, 200, []any{})
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleAgentRollback(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	name := r.PathValue("name")
	version := r.PathValue("version")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.rollback",
		Payload: map[string]string{"tenant": tid, "name": name, "version": version},
	})
	if err != nil {
		RespondError(w, 500, "ROLLBACK_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"status": "rolled_back", "message": resp.Payload.(string)})
}

func (s *Server) handleAgentChatStream(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	agentName := r.PathValue("name")
	message := r.URL.Query().Get("message")

	if message == "" {
		RespondError(w, 400, "INVALID_REQUEST", "message query parameter required")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		RespondError(w, 500, "STREAMING_NOT_SUPPORTED", "server does not support streaming")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 300*time.Second)
	defer cancel()

	// Send thinking indicator
	fmt.Fprintf(w, "event: message\ndata: {\"type\":\"thinking\",\"content\":\"\"}\n\n")
	flusher.Flush()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent:   agentName,
			Tenant:  tid,
			Message: message,
		},
		TraceID: traceID(r),
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

	// Stream the response by sentences for a more natural streaming feel
	content := chatResp.Content
	sentences := splitIntoStreamChunks(content)
	for _, chunk := range sentences {
		data, _ := json.Marshal(map[string]any{
			"type":    "text",
			"content": chunk,
		})
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(data))
		flusher.Flush()
		time.Sleep(30 * time.Millisecond) // small delay for visual streaming effect
	}

	// Send tools used if any
	if len(chatResp.ToolsUsed) > 0 {
		data, _ := json.Marshal(map[string]any{
			"type":       "tools_used",
			"tools_used": chatResp.ToolsUsed,
		})
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", string(data))
		flusher.Flush()
	}

	fmt.Fprintf(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

// splitIntoStreamChunks splits text into natural-looking streaming chunks.
// Splits on sentence boundaries (. ! ? newline) with a minimum chunk size.
func splitIntoStreamChunks(text string) []string {
	if len(text) <= 100 {
		return []string{text}
	}

	var chunks []string
	current := ""

	for _, r := range text {
		current += string(r)
		// Split on sentence boundaries when chunk is long enough
		if len(current) >= 20 && (r == '.' || r == '!' || r == '?' || r == '\n') {
			chunks = append(chunks, current)
			current = ""
		}
		// Hard split at 200 chars if no sentence boundary found
		if len(current) >= 200 {
			chunks = append(chunks, current)
			current = ""
		}
	}
	if current != "" {
		chunks = append(chunks, current)
	}
	return chunks
}
