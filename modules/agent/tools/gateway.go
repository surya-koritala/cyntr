package tools

import (
	"os"
	"strings"
)

// Bundled Tool Gateway (D19).
//
// A single configured gateway (base URL + key) can provide web search, image
// generation, text-to-speech, and a cloud browser — so an operator doesn't
// collect a separate key per capability (the Nous-Portal pattern). It's a pure
// routing layer: when a gateway covers a capability and the tool has no
// explicit per-vendor key, the tool sends its normal request to the gateway
// (which proxies the vendor protocol) using the gateway key. A per-tool key
// always wins, and with no gateway everything behaves exactly as before.

// Gateway capability names.
const (
	CapSearch  = "search"
	CapImage   = "image"
	CapTTS     = "tts"
	CapBrowser = "browser"
)

// ToolGateway is the configured gateway endpoint.
type ToolGateway struct {
	BaseURL string
	Key     string
	caps    map[string]bool
}

// ToolGatewayFromEnv builds a gateway from CYNTR_TOOL_GATEWAY_URL /
// CYNTR_TOOL_GATEWAY_KEY, returning nil when either is unset. The optional
// CYNTR_TOOL_GATEWAY_CAPS (comma-separated) limits which capabilities route
// through it; unset means all four.
func ToolGatewayFromEnv() *ToolGateway {
	base := strings.TrimSpace(os.Getenv("CYNTR_TOOL_GATEWAY_URL"))
	key := strings.TrimSpace(os.Getenv("CYNTR_TOOL_GATEWAY_KEY"))
	if base == "" || key == "" {
		return nil
	}
	caps := map[string]bool{}
	if raw := strings.TrimSpace(os.Getenv("CYNTR_TOOL_GATEWAY_CAPS")); raw != "" {
		for _, c := range strings.Split(raw, ",") {
			if c = strings.TrimSpace(c); c != "" {
				caps[c] = true
			}
		}
	} else {
		caps = map[string]bool{CapSearch: true, CapImage: true, CapTTS: true, CapBrowser: true}
	}
	return &ToolGateway{BaseURL: strings.TrimRight(base, "/"), Key: key, caps: caps}
}

// Has reports whether the gateway routes the given capability.
func (g *ToolGateway) Has(capability string) bool {
	return g != nil && g.caps[capability]
}

// Endpoint resolves the base URL and auth key a tool should use for a
// capability. Precedence: an explicit per-vendor key (vendorKey != "") always
// wins; otherwise the gateway if it covers the capability; otherwise the
// vendor default. The bool reports whether the gateway was chosen.
func (g *ToolGateway) Endpoint(capability, vendorURL, vendorKey string) (string, string, bool) {
	if vendorKey != "" {
		return vendorURL, vendorKey, false
	}
	if g.Has(capability) {
		return g.BaseURL, g.Key, true
	}
	return vendorURL, vendorKey, false
}
