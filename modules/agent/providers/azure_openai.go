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

// AzureOpenAI is a provider for Azure AI Foundry / Azure OpenAI Service.
// URL format: https://{resource}.openai.azure.com/openai/deployments/{deployment}/chat/completions?api-version={version}
// Auth: api-key header (not Bearer token).
type AzureOpenAI struct {
	apiKey     string
	endpoint   string // e.g. https://myresource.openai.azure.com
	deployment string // deployment name in Azure
	apiVersion string
	client     *http.Client
}

func NewAzureOpenAI(apiKey, endpoint, deployment, apiVersion string) *AzureOpenAI {
	if apiVersion == "" {
		apiVersion = "2024-08-01-preview"
	}
	return &AzureOpenAI{
		apiKey:     apiKey,
		endpoint:   strings.TrimRight(endpoint, "/"),
		deployment: deployment,
		apiVersion: apiVersion,
		client:     &http.Client{},
	}
}

func (a *AzureOpenAI) Name() string { return "azure-openai" }

func (a *AzureOpenAI) chatURL() string {
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		a.endpoint, a.deployment, a.apiVersion)
}

func (a *AzureOpenAI) Chat(ctx context.Context, messages []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	reqBody := a.buildRequest(messages, tools)
	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", a.chatURL(), bytes.NewReader(body))
	if err != nil {
		return agent.Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", a.apiKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return agent.Message{}, fmt.Errorf("Azure OpenAI API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return agent.Message{}, fmt.Errorf("Azure OpenAI error %d: %s", resp.StatusCode, string(b))
	}

	var apiResp openaiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return agent.Message{}, fmt.Errorf("decode: %w", err)
	}

	return a.parseResponse(apiResp), nil
}

func (a *AzureOpenAI) buildRequest(messages []agent.Message, tools []agent.ToolDef) openaiRequest {
	// Azure ignores the model field — deployment determines the model.
	// We still send it for compatibility but it's not required.
	req := openaiRequest{Model: a.deployment}

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

func (a *AzureOpenAI) parseResponse(resp openaiResponse) agent.Message {
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
