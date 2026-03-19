package parsers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/proxy"
)

// OpenAIParser extracts intent from OpenAI-compatible API requests.
type OpenAIParser struct{}

func (p *OpenAIParser) Matches(r *http.Request) bool {
	return r.Method == "POST" &&
		strings.HasPrefix(r.URL.Path, "/v1/chat/completions")
}

func (p *OpenAIParser) Parse(r *http.Request) (proxy.Intent, error) {
	intent := proxy.Intent{
		Action:   "model_call",
		Provider: "openai",
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
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(body, &payload); err == nil {
		intent.Model = payload.Model
		if len(payload.Tools) > 0 {
			intent.Tool = payload.Tools[0].Function.Name
		}
	}

	return intent, nil
}
