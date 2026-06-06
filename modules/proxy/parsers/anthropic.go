package parsers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// maxIntentBodyBytes bounds how much of the request body the parser will read
// when sniffing intent. The body is restored for downstream forwarding, so this
// only caps the in-memory buffer used for parsing and prevents a single large
// request from exhausting memory.
const maxIntentBodyBytes = 1 << 20 // 1 MiB

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

	// Read only a bounded prefix of the body for intent sniffing instead of
	// io.ReadAll, which would buffer an attacker-controlled, unbounded body.
	// The prefix is stitched back in front of any unread remainder so the full
	// body is still forwarded downstream unchanged.
	prefix, err := io.ReadAll(io.LimitReader(r.Body, maxIntentBodyBytes))
	if err != nil {
		return intent, nil
	}
	r.Body = struct {
		io.Reader
		io.Closer
	}{
		Reader: io.MultiReader(bytes.NewReader(prefix), r.Body),
		Closer: r.Body,
	}
	body := prefix

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
