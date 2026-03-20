package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/modules/proxy/parsers"
)

// Handler is the HTTP handler for the Proxy Gateway.
// It enforces policy on every request before forwarding to upstream.
type Handler struct {
	bus           *ipc.Bus
	upstreamURL   string
	parsers       []parsers.IntentParser
	policyTimeout time.Duration
}

// NewHandler creates a proxy handler that checks policy before forwarding.
func NewHandler(bus *ipc.Bus, upstreamURL string) *Handler {
	return &Handler{
		bus:           bus,
		upstreamURL:   upstreamURL,
		policyTimeout: 5 * time.Second,
		parsers: []parsers.IntentParser{
			&parsers.AnthropicParser{},
			&parsers.OpenAIParser{},
		},
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Health endpoint — no policy check needed
	if r.URL.Path == "/health" {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	tenant := r.Header.Get("X-Cyntr-Tenant")
	user := r.Header.Get("X-Cyntr-User")

	// Extract intent from request
	intent := h.extractIntent(r)

	// Check policy — fail-closed
	ctx, cancel := context.WithTimeout(r.Context(), h.policyTimeout)
	defer cancel()

	resp, err := h.bus.Request(ctx, ipc.Message{
		Source: "proxy",
		Target: "policy",
		Topic:  "policy.check",
		Payload: policy.CheckRequest{
			Tenant: tenant,
			Action: intent.Action,
			Tool:   intent.Tool,
			User:   user,
		},
	})
	if err != nil {
		// Fail-closed: if policy engine is unavailable, deny
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "policy engine unavailable",
		})
		return
	}

	checkResp, ok := resp.Payload.(policy.CheckResponse)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if checkResp.Decision != policy.Allow {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{
			"error":  "policy denied",
			"rule":   checkResp.Rule,
			"reason": checkResp.Reason,
		})
		return
	}

	// Forward to upstream
	upstream, err := url.Parse(h.upstreamURL)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(upstream)
	proxy.ServeHTTP(w, r)
}

func (h *Handler) extractIntent(r *http.Request) Intent {
	for _, p := range h.parsers {
		if p.Matches(r) {
			parsed, err := p.Parse(r)
			if err == nil {
				// Convert parsers.Intent to proxy.Intent (same fields, different types)
				return Intent{
					Action:   parsed.Action,
					Provider: parsed.Provider,
					Model:    parsed.Model,
					Tool:     parsed.Tool,
				}
			}
		}
	}
	// Fallback: unknown intent
	return Intent{
		Action: "unknown",
	}
}
