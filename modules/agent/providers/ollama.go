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

type Ollama struct {
	model   string
	baseURL string
	client  *http.Client
}

func NewOllama(model, baseURL string) *Ollama {
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Ollama{
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{},
	}
}

func (o *Ollama) Name() string { return o.model }

func (o *Ollama) Chat(ctx context.Context, messages []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	reqBody := o.buildRequest(messages, tools)
	body, err := json.Marshal(reqBody)
	if err != nil {
		return agent.Message{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return agent.Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return agent.Message{}, fmt.Errorf("Ollama API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return agent.Message{}, fmt.Errorf("Ollama error %d: %s", resp.StatusCode, string(b))
	}

	var result ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return agent.Message{}, fmt.Errorf("decode: %w", err)
	}

	return o.parseResponse(result), nil
}

func (o *Ollama) buildRequest(messages []agent.Message, tools []agent.ToolDef) ollamaRequest {
	req := ollamaRequest{Model: o.model, Stream: false}

	for _, msg := range messages {
		switch msg.Role {
		case agent.RoleSystem:
			req.Messages = append(req.Messages, ollamaMsg{Role: "system", Content: msg.Content})
		case agent.RoleUser:
			req.Messages = append(req.Messages, ollamaMsg{Role: "user", Content: msg.Content})
		case agent.RoleAssistant:
			if len(msg.ToolCalls) > 0 {
				var tcs []ollamaToolCall
				for _, tc := range msg.ToolCalls {
					args := make(map[string]any, len(tc.Input))
					for k, v := range tc.Input {
						args[k] = v
					}
					tcs = append(tcs, ollamaToolCall{
						Function: ollamaFnCall{
							Name:      tc.Name,
							Arguments: args,
						},
					})
				}
				req.Messages = append(req.Messages, ollamaMsg{Role: "assistant", ToolCalls: tcs})
			} else {
				req.Messages = append(req.Messages, ollamaMsg{Role: "assistant", Content: msg.Content})
			}
		case agent.RoleTool:
			for _, tr := range msg.ToolResults {
				req.Messages = append(req.Messages, ollamaMsg{Role: "tool", Content: tr.Content})
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
		req.Tools = append(req.Tools, ollamaTool{
			Type: "function",
			Function: ollamaFnDef{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  params,
			},
		})
	}

	return req
}

func (o *Ollama) parseResponse(resp ollamaResponse) agent.Message {
	msg := agent.Message{Role: agent.RoleAssistant}

	if resp.PromptEvalCount > 0 {
		msg.InputTokens = resp.PromptEvalCount
	}
	if resp.EvalCount > 0 {
		msg.OutputTokens = resp.EvalCount
	}

	msg.Content = resp.Message.Content

	for _, tc := range resp.Message.ToolCalls {
		toolCall := agent.ToolCall{
			ID:    fmt.Sprintf("call_%s_%d", tc.Function.Name, len(msg.ToolCalls)),
			Name:  tc.Function.Name,
			Input: make(map[string]string),
		}
		for k, v := range tc.Function.Arguments {
			toolCall.Input[k] = fmt.Sprintf("%v", v)
		}
		msg.ToolCalls = append(msg.ToolCalls, toolCall)
	}
	return msg
}

// Ollama API types

type ollamaRequest struct {
	Model    string       `json:"model"`
	Messages []ollamaMsg  `json:"messages"`
	Stream   bool         `json:"stream"`
	Tools    []ollamaTool `json:"tools,omitempty"`
}

type ollamaMsg struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
}

type ollamaToolCall struct {
	Function ollamaFnCall `json:"function"`
}

type ollamaFnCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ollamaTool struct {
	Type     string      `json:"type"`
	Function ollamaFnDef `json:"function"`
}

type ollamaFnDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type ollamaResponse struct {
	Message struct {
		Role      string           `json:"role"`
		Content   string           `json:"content"`
		ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	} `json:"message"`
	PromptEvalCount int `json:"prompt_eval_count"`
	EvalCount       int `json:"eval_count"`
}
