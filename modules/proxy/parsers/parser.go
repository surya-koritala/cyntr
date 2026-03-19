package parsers

import (
	"net/http"

	"github.com/cyntr-dev/cyntr/modules/proxy"
)

// IntentParser extracts semantic intent from HTTP requests.
type IntentParser interface {
	// Matches returns true if this parser can handle the request.
	Matches(r *http.Request) bool
	// Parse extracts the intent from the request.
	Parse(r *http.Request) (proxy.Intent, error)
}
