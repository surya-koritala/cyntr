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

func (o *Ollama) Name() string { return "ollama" }

func (o *Ollama) Chat(ctx context.Context, messages []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	var msgs []map[string]string
	for _, m := range messages {
		msgs = append(msgs, map[string]string{"role": m.Role.String(), "content": m.Content})
	}

	reqBody := map[string]any{"model": o.model, "messages": msgs, "stream": false}
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

	var result struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return agent.Message{}, fmt.Errorf("decode: %w", err)
	}

	return agent.Message{Role: agent.RoleAssistant, Content: result.Message.Content}, nil
}
