# Anthropic Adapter + Streaming Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a real Claude/Anthropic model provider with streaming support, so Cyntr can make actual LLM calls and stream tokens to users in real time.

**Architecture:** Extend the `ModelProvider` interface with an optional `StreamingProvider` interface. The Anthropic adapter calls the Claude Messages API (`POST /v1/messages`) with `stream: true`, parses SSE events, and yields tokens through a Go channel. The existing `Chat()` method still works (collects all tokens then returns). The REST API gets a streaming chat endpoint using Server-Sent Events (SSE).

**Tech Stack:** Go 1.22+ stdlib `net/http`. Raw HTTP to Anthropic API — no SDK dependency.

**Spec:** `docs/superpowers/specs/2026-03-19-cyntr-enterprise-platform-design.md` (Section 2.3)

---

## File Structure

```
modules/agent/
├── provider.go            # Add StreamingProvider interface (MODIFY)
├── providers/
│   ├── mock.go            # Unchanged
│   └── anthropic.go       # NEW: real Claude API adapter with streaming
├── providers/
│   └── anthropic_test.go  # NEW: tests with httptest mock Anthropic server
web/api/
├── agents.go              # Add SSE streaming chat endpoint (MODIFY)
```

---

## Chunk 1: Streaming Interface + Anthropic Adapter

### Task 1: Add StreamingProvider Interface

**Files:**
- Modify: `modules/agent/provider.go`

- [ ] **Step 1: Add streaming types and interface**

Add to `modules/agent/provider.go`:
```go
// StreamChunk represents a piece of a streaming response.
type StreamChunk struct {
	Type      string // "text", "tool_use_start", "tool_use_input", "done", "error"
	Text      string // text content (for "text" chunks)
	ToolCall  *ToolCall // set on "tool_use_start"
	ToolInput string    // partial JSON input (for "tool_use_input")
	Error     error     // set on "error" chunks
}

// StreamingProvider extends ModelProvider with streaming support.
// Providers that support streaming implement this in addition to ModelProvider.
type StreamingProvider interface {
	ModelProvider
	// ChatStream sends messages and returns a channel of streaming chunks.
	// The channel is closed when the response is complete.
	// The caller must drain the channel.
	ChatStream(ctx context.Context, messages []Message, tools []ToolDef) (<-chan StreamChunk, error)
}
```

- [ ] **Step 2: Verify existing tests still pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/... -v -count=1`
Expected: All PASS (no breaking changes)

- [ ] **Step 3: Commit**

```bash
git add modules/agent/provider.go
git commit -m "feat(agent): add StreamingProvider interface and StreamChunk type"
```

---

### Task 2: Implement Anthropic Provider

**Files:**
- Create: `modules/agent/providers/anthropic.go`
- Create: `modules/agent/providers/anthropic_test.go`

- [ ] **Step 1: Write failing tests using httptest to mock the Anthropic API**

Create `modules/agent/providers/anthropic_test.go`:
```go
package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

func mockAnthropicServer(response string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format
		if r.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		if r.Header.Get("X-API-Key") == "" {
			http.Error(w, "missing API key", 401)
			return
		}
		if r.Header.Get("Anthropic-Version") == "" {
			http.Error(w, "missing version", 400)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, response)
	}))
}

func mockAnthropicStreamServer(events []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", 500)
			return
		}

		for _, event := range events {
			fmt.Fprint(w, event)
			flusher.Flush()
		}
	}))
}

func TestAnthropicProviderName(t *testing.T) {
	p := NewAnthropic("test-key", "claude-sonnet-4-20250514", "")
	if p.Name() != "claude" {
		t.Fatalf("expected 'claude', got %q", p.Name())
	}
}

func TestAnthropicProviderChat(t *testing.T) {
	server := mockAnthropicServer(`{
		"id": "msg_01",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello! How can I help?"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 15}
	}`)
	defer server.Close()

	p := NewAnthropic("test-key", "claude-sonnet-4-20250514", server.URL)

	resp, err := p.Chat(context.Background(), []agent.Message{
		{Role: agent.RoleUser, Content: "Hello"},
	}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Role != agent.RoleAssistant {
		t.Fatalf("expected assistant, got %s", resp.Role)
	}
	if resp.Content != "Hello! How can I help?" {
		t.Fatalf("expected greeting, got %q", resp.Content)
	}
}

func TestAnthropicProviderChatWithToolUse(t *testing.T) {
	server := mockAnthropicServer(`{
		"id": "msg_02",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Let me check that."},
			{"type": "tool_use", "id": "toolu_01", "name": "get_weather", "input": {"city": "London"}}
		],
		"stop_reason": "tool_use"
	}`)
	defer server.Close()

	p := NewAnthropic("test-key", "claude-sonnet-4-20250514", server.URL)

	resp, err := p.Chat(context.Background(), []agent.Message{
		{Role: agent.RoleUser, Content: "What's the weather?"},
	}, []agent.ToolDef{{Name: "get_weather", Description: "Get weather"}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("expected get_weather, got %q", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].ID != "toolu_01" {
		t.Fatalf("expected toolu_01, got %q", resp.ToolCalls[0].ID)
	}
}

func TestAnthropicProviderChatStream(t *testing.T) {
	server := mockAnthropicStreamServer([]string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"role\":\"assistant\"}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	})
	defer server.Close()

	p := NewAnthropic("test-key", "claude-sonnet-4-20250514", server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := p.ChatStream(ctx, []agent.Message{
		{Role: agent.RoleUser, Content: "Hi"},
	}, nil)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	var fullText string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		if chunk.Type == "text" {
			fullText += chunk.Text
		}
	}

	if fullText != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", fullText)
	}
}

func TestAnthropicProviderStreamToolUse(t *testing.T) {
	server := mockAnthropicStreamServer([]string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"role\":\"assistant\"}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"get_weather\",\"input\":{}}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"city\\\": \\\"London\\\"}\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	})
	defer server.Close()

	p := NewAnthropic("test-key", "claude-sonnet-4-20250514", server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := p.ChatStream(ctx, []agent.Message{
		{Role: agent.RoleUser, Content: "Weather?"},
	}, []agent.ToolDef{{Name: "get_weather"}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	var gotToolStart bool
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("error: %v", chunk.Error)
		}
		if chunk.Type == "tool_use_start" {
			gotToolStart = true
			if chunk.ToolCall == nil || chunk.ToolCall.Name != "get_weather" {
				t.Fatalf("expected get_weather tool call")
			}
		}
	}
	if !gotToolStart {
		t.Fatal("expected tool_use_start chunk")
	}
}

func TestAnthropicProviderBadAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	}))
	defer server.Close()

	p := NewAnthropic("bad-key", "claude-sonnet-4-20250514", server.URL)

	_, err := p.Chat(context.Background(), []agent.Message{
		{Role: agent.RoleUser, Content: "Hi"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for bad API key")
	}
}

func TestAnthropicProviderImplementsStreaming(t *testing.T) {
	var _ agent.StreamingProvider = (*Anthropic)(nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/providers/ -v -count=1`
Expected: FAIL — `NewAnthropic`, `Anthropic` not defined

- [ ] **Step 3: Implement Anthropic provider**

Create `modules/agent/providers/anthropic.go`:
```go
package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

const (
	defaultAnthropicURL = "https://api.anthropic.com"
	anthropicVersion    = "2023-06-01"
)

// Anthropic is a model provider for the Claude API.
// Implements both ModelProvider and StreamingProvider.
type Anthropic struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// NewAnthropic creates a Claude API provider.
// baseURL can be empty to use the default Anthropic API endpoint.
func NewAnthropic(apiKey, model, baseURL string) *Anthropic {
	if baseURL == "" {
		baseURL = defaultAnthropicURL
	}
	return &Anthropic{
		apiKey:  apiKey,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{},
	}
}

func (a *Anthropic) Name() string { return "claude" }

// Chat sends a non-streaming request and returns the complete response.
func (a *Anthropic) Chat(ctx context.Context, messages []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	reqBody := a.buildRequest(messages, tools, false)

	body, err := json.Marshal(reqBody)
	if err != nil {
		return agent.Message{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return agent.Message{}, fmt.Errorf("create request: %w", err)
	}
	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return agent.Message{}, fmt.Errorf("API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return agent.Message{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var apiResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return agent.Message{}, fmt.Errorf("decode response: %w", err)
	}

	return a.parseResponse(apiResp), nil
}

// ChatStream sends a streaming request and returns a channel of chunks.
func (a *Anthropic) ChatStream(ctx context.Context, messages []agent.Message, tools []agent.ToolDef) (<-chan agent.StreamChunk, error) {
	reqBody := a.buildRequest(messages, tools, true)

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	a.setHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API call: %w", err)
	}

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan agent.StreamChunk, 64)

	go func() {
		defer close(ch)
		defer resp.Body.Close()
		a.parseSSEStream(ctx, resp.Body, ch)
	}()

	return ch, nil
}

func (a *Anthropic) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", a.apiKey)
	req.Header.Set("Anthropic-Version", anthropicVersion)
}

// --- Request building ---

type anthropicRequest struct {
	Model     string            `json:"model"`
	Messages  []anthropicMsg    `json:"messages"`
	System    string            `json:"system,omitempty"`
	MaxTokens int               `json:"max_tokens"`
	Stream    bool              `json:"stream,omitempty"`
	Tools     []anthropicTool   `json:"tools,omitempty"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

func (a *Anthropic) buildRequest(messages []agent.Message, tools []agent.ToolDef, stream bool) anthropicRequest {
	req := anthropicRequest{
		Model:     a.model,
		MaxTokens: 4096,
		Stream:    stream,
	}

	for _, msg := range messages {
		switch msg.Role {
		case agent.RoleSystem:
			req.System = msg.Content
		case agent.RoleUser:
			req.Messages = append(req.Messages, anthropicMsg{Role: "user", Content: msg.Content})
		case agent.RoleAssistant:
			if len(msg.ToolCalls) > 0 {
				// Assistant message with tool use
				var content []map[string]any
				if msg.Content != "" {
					content = append(content, map[string]any{"type": "text", "text": msg.Content})
				}
				for _, tc := range msg.ToolCalls {
					content = append(content, map[string]any{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Name,
						"input": tc.Input,
					})
				}
				req.Messages = append(req.Messages, anthropicMsg{Role: "assistant", Content: content})
			} else {
				req.Messages = append(req.Messages, anthropicMsg{Role: "assistant", Content: msg.Content})
			}
		case agent.RoleTool:
			for _, tr := range msg.ToolResults {
				req.Messages = append(req.Messages, anthropicMsg{
					Role: "user",
					Content: []map[string]any{{
						"type":       "tool_result",
						"tool_use_id": tr.CallID,
						"content":    tr.Content,
						"is_error":   tr.IsError,
					}},
				})
			}
		}
	}

	for _, tool := range tools {
		schema := map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
		var required []string
		for name, param := range tool.Parameters {
			schema["properties"].(map[string]any)[name] = map[string]any{
				"type":        param.Type,
				"description": param.Description,
			}
			if param.Required {
				required = append(required, name)
			}
		}
		if len(required) > 0 {
			schema["required"] = required
		}

		req.Tools = append(req.Tools, anthropicTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: schema,
		})
	}

	return req
}

// --- Response parsing (non-streaming) ---

type anthropicResponse struct {
	ID      string           `json:"id"`
	Content []contentBlock   `json:"content"`
	Model   string           `json:"model"`
	Usage   *anthropicUsage  `json:"usage"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func (a *Anthropic) parseResponse(resp anthropicResponse) agent.Message {
	msg := agent.Message{Role: agent.RoleAssistant}

	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			msg.Content += block.Text
		case "tool_use":
			tc := agent.ToolCall{
				ID:   block.ID,
				Name: block.Name,
			}
			// Parse input JSON to map[string]string
			var inputMap map[string]any
			if err := json.Unmarshal(block.Input, &inputMap); err == nil {
				tc.Input = make(map[string]string)
				for k, v := range inputMap {
					tc.Input[k] = fmt.Sprintf("%v", v)
				}
			}
			msg.ToolCalls = append(msg.ToolCalls, tc)
		}
	}

	return msg
}

// --- SSE stream parsing ---

type sseEvent struct {
	Event string
	Data  string
}

func (a *Anthropic) parseSSEStream(ctx context.Context, body io.Reader, ch chan<- agent.StreamChunk) {
	scanner := bufio.NewScanner(body)
	var currentEvent sseEvent

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			ch <- agent.StreamChunk{Type: "error", Error: ctx.Err()}
			return
		default:
		}

		line := scanner.Text()

		if line == "" {
			// Empty line = event boundary, process the event
			if currentEvent.Data != "" {
				a.processSSEEvent(currentEvent, ch)
			}
			currentEvent = sseEvent{}
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			currentEvent.Event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentEvent.Data = strings.TrimPrefix(line, "data: ")
		}
	}
}

func (a *Anthropic) processSSEEvent(event sseEvent, ch chan<- agent.StreamChunk) {
	var data map[string]any
	if err := json.Unmarshal([]byte(event.Data), &data); err != nil {
		return
	}

	eventType, _ := data["type"].(string)

	switch eventType {
	case "content_block_start":
		block, ok := data["content_block"].(map[string]any)
		if !ok {
			return
		}
		blockType, _ := block["type"].(string)
		if blockType == "tool_use" {
			ch <- agent.StreamChunk{
				Type: "tool_use_start",
				ToolCall: &agent.ToolCall{
					ID:   strVal(block, "id"),
					Name: strVal(block, "name"),
				},
			}
		}

	case "content_block_delta":
		delta, ok := data["delta"].(map[string]any)
		if !ok {
			return
		}
		deltaType, _ := delta["type"].(string)
		switch deltaType {
		case "text_delta":
			ch <- agent.StreamChunk{
				Type: "text",
				Text: strVal(delta, "text"),
			}
		case "input_json_delta":
			ch <- agent.StreamChunk{
				Type:      "tool_use_input",
				ToolInput: strVal(delta, "partial_json"),
			}
		}

	case "message_stop":
		ch <- agent.StreamChunk{Type: "done"}
	}
}

func strVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/providers/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/agent/providers/anthropic.go modules/agent/providers/anthropic_test.go modules/agent/provider.go
git commit -m "feat(agent): implement Anthropic/Claude provider with streaming SSE support"
```

---

## Chunk 2: Streaming Chat API Endpoint

### Task 3: Add SSE Streaming Chat Endpoint

**Files:**
- Modify: `web/api/agents.go`
- Modify: `web/api/server.go`

- [ ] **Step 1: Add streaming endpoint to server routes**

Add to `registerRoutes()` in `web/api/server.go`:
```go
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents/{name}/stream", s.handleAgentChatStream)
```

- [ ] **Step 2: Implement SSE streaming handler**

Add to `web/api/agents.go`:
```go
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
```

Add the missing imports to agents.go: `"fmt"` and `"github.com/cyntr-dev/cyntr/kernel/ipc"`.

- [ ] **Step 3: Run all tests**

Run: `cd /Users/suryakoritala/Cyntr && go test ./... -count=1 -race`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add web/api/agents.go web/api/server.go
git commit -m "feat(api): add SSE streaming chat endpoint for real-time token delivery"
```

---

### Task 4: Update CLI to Register Anthropic Provider

**Files:**
- Modify: `cmd/cyntr/main.go`

- [ ] **Step 1: Add Anthropic provider registration**

In `runStart()` in `cmd/cyntr/main.go`, after creating `agentRuntime`, add:
```go
	// Register model providers
	agentRuntime.RegisterProvider(agentproviders.NewMock("Default mock response"))

	// Register Claude provider if API key is set
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey != "" {
		claudeModel := os.Getenv("ANTHROPIC_MODEL")
		if claudeModel == "" {
			claudeModel = "claude-sonnet-4-20250514"
		}
		agentRuntime.RegisterProvider(agentproviders.NewAnthropic(anthropicKey, claudeModel, ""))
		fmt.Printf("registered Claude provider (model: %s)\n", claudeModel)
	}
```

- [ ] **Step 2: Build and verify**

Run: `cd /Users/suryakoritala/Cyntr && go build -o cyntr ./cmd/cyntr && ./cyntr version`
Expected: `cyntr v0.1.0`

- [ ] **Step 3: Commit**

```bash
git add cmd/cyntr/main.go
git commit -m "feat(cli): register Anthropic/Claude provider when ANTHROPIC_API_KEY is set"
```

---

### Task 5: Final Verification

- [ ] **Step 1: Run complete test suite**

Run: `cd /Users/suryakoritala/Cyntr && go test ./... -count=1 -race`
Expected: All PASS

- [ ] **Step 2: Run go vet**

Run: `cd /Users/suryakoritala/Cyntr && go vet ./...`

- [ ] **Step 3: Build binary**

Run: `cd /Users/suryakoritala/Cyntr && go build -o cyntr ./cmd/cyntr && ./cyntr version`
Expected: `cyntr v0.1.0`
