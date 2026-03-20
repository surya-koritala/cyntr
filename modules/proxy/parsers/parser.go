package parsers

import (
	"net/http"
)

// Intent represents the semantic meaning extracted from an HTTP request.
// This mirrors proxy.Intent but is defined here to avoid an import cycle.
type Intent struct {
	Action   string // "model_call", "tool_call", "unknown"
	Provider string // "anthropic", "openai", ""
	Model    string // model name if detected
	Tool     string // tool name if detected
}

// IntentParser extracts semantic intent from HTTP requests.
type IntentParser interface {
	// Matches returns true if this parser can handle the request.
	Matches(r *http.Request) bool
	// Parse extracts the intent from the request.
	Parse(r *http.Request) (Intent, error)
}
