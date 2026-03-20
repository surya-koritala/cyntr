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

// Gemini is a model provider for Google's Gemini API.
type Gemini struct {
	apiKey  string
	model   string
	baseURL string
	client  *http.Client
}

// NewGemini creates a Gemini API provider.
// baseURL can be empty to use the default Gemini API endpoint.
func NewGemini(apiKey, model, baseURL string) *Gemini {
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}
	if model == "" {
		model = "gemini-pro"
	}
	return &Gemini{
		apiKey:  apiKey,
		model:   model,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{},
	}
}

func (g *Gemini) Name() string { return "gemini" }

// Chat sends a request to the Gemini generateContent API and returns the response.
func (g *Gemini) Chat(ctx context.Context, messages []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	var contents []map[string]any
	for _, m := range messages {
		role := "user"
		if m.Role == agent.RoleAssistant {
			role = "model"
		}
		if m.Role == agent.RoleSystem {
			continue // Gemini handles system instructions differently
		}
		contents = append(contents, map[string]any{
			"role":  role,
			"parts": []map[string]string{{"text": m.Content}},
		})
	}

	reqBody := map[string]any{"contents": contents}
	body, _ := json.Marshal(reqBody)

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", g.baseURL, g.model, g.apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return agent.Message{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return agent.Message{}, fmt.Errorf("Gemini API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return agent.Message{}, fmt.Errorf("Gemini error %d: %s", resp.StatusCode, b)
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	msg := agent.Message{Role: agent.RoleAssistant}
	if len(result.Candidates) > 0 {
		for _, part := range result.Candidates[0].Content.Parts {
			msg.Content += part.Text
		}
	}
	return msg, nil
}
