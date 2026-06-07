package proxy

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
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

	// identitySecret is the shared secret used to authenticate the caller's
	// tenant/user identity via an HMAC-SHA256 signature. When set, the
	// identity used for the policy decision is derived ONLY from the verified
	// signed credential; the raw X-Cyntr-Tenant/X-Cyntr-User headers are
	// ignored. When empty, the gateway falls back to trusting those headers
	// (development / behind a trusted authenticating proxy only).
	identitySecret string

	// proxy is the reverse proxy, constructed once and reused across requests.
	proxy *httputil.ReverseProxy
}

// NewHandler creates a proxy handler that checks policy before forwarding.
func NewHandler(bus *ipc.Bus, upstreamURL string) *Handler {
	h := &Handler{
		bus:           bus,
		upstreamURL:   upstreamURL,
		policyTimeout: 5 * time.Second,
		parsers: []parsers.IntentParser{
			&parsers.AnthropicParser{},
			&parsers.OpenAIParser{},
		},
	}

	// Build the reverse proxy once. A per-request proxy leaks the underlying
	// transport's idle connections and recreates state on every call.
	if upstream, err := url.Parse(upstreamURL); err == nil {
		proxy := httputil.NewSingleHostReverseProxy(upstream)
		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			// Fix Host header so upstream (e.g. api.anthropic.com) accepts it.
			req.Host = upstream.Host
		}
		h.proxy = proxy
	}

	return h
}

// SetIdentitySecret configures the shared secret used to authenticate the
// caller-supplied tenant/user identity. See Handler.identitySecret.
func (h *Handler) SetIdentitySecret(secret string) {
	h.identitySecret = secret
}

// authenticatedIdentity returns the tenant and user for the policy decision.
//
// When an identity secret is configured, the identity is taken ONLY from the
// X-Cyntr-Identity header ("<tenant>:<user>"), whose value must be signed by
// the X-Cyntr-Identity-Sig header (hex HMAC-SHA256 over the identity string,
// keyed by the secret) and verified in constant time. A missing or invalid
// signature fails closed (empty identity), so the policy engine cannot be
// fed attacker-controlled tenant/user values via raw headers.
//
// When no secret is configured, the legacy X-Cyntr-Tenant/X-Cyntr-User headers
// are used as-is (trusted-proxy / development mode).
func (h *Handler) authenticatedIdentity(r *http.Request) (tenant, user string) {
	if h.identitySecret == "" {
		return r.Header.Get("X-Cyntr-Tenant"), r.Header.Get("X-Cyntr-User")
	}

	identity := r.Header.Get("X-Cyntr-Identity")
	sig := r.Header.Get("X-Cyntr-Identity-Sig")
	if identity == "" || sig == "" {
		return "", ""
	}

	mac := hmac.New(sha256.New, []byte(h.identitySecret))
	mac.Write([]byte(identity))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", ""
	}

	tenant, user, _ = strings.Cut(identity, ":")
	return tenant, user
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Health endpoint — no policy check needed
	if r.URL.Path == "/health" {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	// Derive the tenant/user identity for the policy decision from an
	// authenticated credential rather than trusting raw, attacker-settable
	// headers. Fails closed (empty identity) when a secret is configured but
	// the signature is missing/invalid.
	tenant, user := h.authenticatedIdentity(r)

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

	// Forward to upstream using the reverse proxy built once in NewHandler.
	// A nil proxy means the upstream URL was unparseable at construction time.
	if h.proxy == nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	h.proxy.ServeHTTP(w, r)
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
