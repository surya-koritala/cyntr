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
	Model     string          `json:"model"`
	Messages  []anthropicMsg  `json:"messages"`
	System    string          `json:"system,omitempty"`
	MaxTokens int             `json:"max_tokens"`
	Stream    bool            `json:"stream,omitempty"`
	Tools     []anthropicTool `json:"tools,omitempty"`
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
						"type":        "tool_result",
						"tool_use_id": tr.CallID,
						"content":     tr.Content,
						"is_error":    tr.IsError,
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
	ID      string          `json:"id"`
	Content []contentBlock  `json:"content"`
	Model   string          `json:"model"`
	Usage   *anthropicUsage `json:"usage"`
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
