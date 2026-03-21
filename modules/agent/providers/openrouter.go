package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type OpenRouter struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

func NewOpenRouter(apiKey, model, baseURL string) *OpenRouter {
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api"
	}
	return &OpenRouter{
		apiKey:  apiKey,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{},
	}
}

func (o *OpenRouter) Name() string { return "openrouter" }

func (o *OpenRouter) Chat(ctx context.Context, messages []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	reqBody := o.buildRequest(messages, tools)
	body, err := json.Marshal(reqBody)
	if err != nil {
		return agent.Message{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return agent.Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("HTTP-Referer", "https://cyntr.dev")
	req.Header.Set("X-Title", "Cyntr")

	resp, err := o.client.Do(req)
	if err != nil {
		return agent.Message{}, fmt.Errorf("OpenRouter API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return agent.Message{}, fmt.Errorf("OpenRouter error %d: %s", resp.StatusCode, string(b))
	}

	var apiResp openaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return agent.Message{}, fmt.Errorf("decode: %w", err)
	}

	return o.parseResponse(apiResp), nil
}

func (o *OpenRouter) buildRequest(messages []agent.Message, tools []agent.ToolDef) openaiRequest {
	req := openaiRequest{Model: o.model}

	for _, msg := range messages {
		switch msg.Role {
		case agent.RoleSystem:
			req.Messages = append(req.Messages, openaiMsg{Role: "system", Content: msg.Content})
		case agent.RoleUser:
			req.Messages = append(req.Messages, openaiMsg{Role: "user", Content: msg.Content})
		case agent.RoleAssistant:
			m := openaiMsg{Role: "assistant", Content: msg.Content}
			for _, tc := range msg.ToolCalls {
				inputJSON, _ := json.Marshal(tc.Input)
				m.ToolCalls = append(m.ToolCalls, openaiToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{Name: tc.Name, Arguments: string(inputJSON)},
				})
			}
			req.Messages = append(req.Messages, m)
		case agent.RoleTool:
			for _, tr := range msg.ToolResults {
				req.Messages = append(req.Messages, openaiMsg{
					Role:       "tool",
					Content:    tr.Content,
					ToolCallID: tr.CallID,
				})
			}
		}
	}

	for _, tool := range tools {
		params := map[string]any{"type": "object", "properties": map[string]any{}}
		var required []string
		for name, p := range tool.Parameters {
			params["properties"].(map[string]any)[name] = map[string]any{"type": p.Type, "description": p.Description}
			if p.Required {
				required = append(required, name)
			}
		}
		if len(required) > 0 {
			params["required"] = required
		}
		req.Tools = append(req.Tools, openaiTool{
			Type: "function",
			Function: openaiFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
			},
		})
	}

	return req
}

func (o *OpenRouter) parseResponse(resp openaiResponse) agent.Message {
	msg := agent.Message{Role: agent.RoleAssistant}
	if len(resp.Choices) == 0 {
		return msg
	}

	choice := resp.Choices[0].Message
	msg.Content = choice.Content

	for _, tc := range choice.ToolCalls {
		toolCall := agent.ToolCall{ID: tc.ID, Name: tc.Function.Name}
		var args map[string]any
		if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil {
			toolCall.Input = make(map[string]string)
			for k, v := range args {
				toolCall.Input[k] = fmt.Sprintf("%v", v)
			}
		}
		msg.ToolCalls = append(msg.ToolCalls, toolCall)
	}
	return msg
}
