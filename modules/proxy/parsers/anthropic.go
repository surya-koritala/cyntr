package parsers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// AnthropicParser extracts intent from Anthropic API requests.
type AnthropicParser struct{}

func (p *AnthropicParser) Matches(r *http.Request) bool {
	return r.Method == "POST" &&
		strings.HasPrefix(r.URL.Path, "/v1/messages") &&
		r.Header.Get("X-API-Key") != ""
}

func (p *AnthropicParser) Parse(r *http.Request) (Intent, error) {
	intent := Intent{
		Action:   "model_call",
		Provider: "anthropic",
	}

	if r.Body == nil {
		return intent, nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return intent, nil
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var payload struct {
		Model string `json:"model"`
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(body, &payload); err == nil {
		intent.Model = payload.Model
		if len(payload.Tools) > 0 {
			intent.Tool = payload.Tools[0].Name
		}
	}

	return intent, nil
}
