package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

// TestHandlerMissingTenantHeader verifies that a request without the
// X-Cyntr-Tenant header still reaches the policy engine (with an empty tenant
// string) and is handled according to the policy decision.
func TestHandlerMissingTenantHeader(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer upstream.Close()

	bus := ipc.NewBus()
	defer bus.Close()

	// Track the tenant the policy engine received.
	var receivedTenant string
	bus.Handle("policy", "policy.check", func(msg ipc.Message) (ipc.Message, error) {
		if req, ok := msg.Payload.(policy.CheckRequest); ok {
			receivedTenant = req.Tenant
		}
		return ipc.Message{
			Type:    ipc.MessageTypeResponse,
			Payload: policy.CheckResponse{Decision: policy.Allow},
		}, nil
	})

	h := NewHandler(bus, upstream.URL)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude"}`))
	// Deliberately omit X-Cyntr-Tenant.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// Request should be allowed (policy returned Allow) and forwarded.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// The policy engine must have been called with an empty tenant.
	if receivedTenant != "" {
		t.Fatalf("expected empty tenant in policy check, got %q", receivedTenant)
	}
}

// TestHandlerInvalidUpstreamURL verifies that the handler returns 500 when the
// upstream URL is syntactically invalid and cannot be parsed by url.Parse.
// A URL containing a raw space is reliably rejected.
func TestHandlerInvalidUpstreamURL(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	setupPolicyHandler(bus, policy.Allow)

	// A URL with a raw space is rejected by url.Parse.
	h := NewHandler(bus, "http://bad host with spaces")

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("X-Cyntr-Tenant", "finance")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	// handler.go returns 500 on url.Parse failure.
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

// TestHandlerIntentExtractionFallback verifies that a request that does not
// match any registered parser results in an "unknown" action being sent to the
// policy engine, and the request is still handled per the policy decision.
func TestHandlerIntentExtractionFallback(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	bus := ipc.NewBus()
	defer bus.Close()

	var receivedAction string
	bus.Handle("policy", "policy.check", func(msg ipc.Message) (ipc.Message, error) {
		if req, ok := msg.Payload.(policy.CheckRequest); ok {
			receivedAction = req.Action
		}
		return ipc.Message{
			Type:    ipc.MessageTypeResponse,
			Payload: policy.CheckResponse{Decision: policy.Allow},
		}, nil
	})

	h := NewHandler(bus, upstream.URL)

	// Use an arbitrary path that none of the parsers recognise.
	req := httptest.NewRequest("GET", "/some/unknown/endpoint", nil)
	req.Header.Set("X-Cyntr-Tenant", "finance")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if receivedAction != "unknown" {
		t.Fatalf("expected action=unknown in policy check, got %q", receivedAction)
	}
}

// TestHandlerPolicyReturnsRequireApproval verifies that a RequireApproval
// decision from the policy engine is treated the same as Deny and returns 403.
func TestHandlerPolicyReturnsRequireApproval(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	setupPolicyHandler(bus, policy.RequireApproval)

	h := NewHandler(bus, "http://unreachable")

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("X-Cyntr-Tenant", "finance")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for require_approval, got %d", w.Code)
	}
}

// TestGatewayRateLimitIntegration verifies the end-to-end rate limiting path:
// a real HTTP server backed by a Handler + RateLimiter returns 429 once the
// per-tenant limit is exhausted.  We use a limit of 3 req/min so the test
// completes without sleeping.
func TestGatewayRateLimitIntegration(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	setupPolicyHandler(bus, policy.Allow)

	// Use a real upstream that always returns 200.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// Wire a Handler through a tight RateLimiter into a real test server.
	handler := NewHandler(bus, upstream.URL)
	rateLimiter := NewRateLimiter(3, 1*time.Minute)
	ts := httptest.NewServer(rateLimiter.Middleware(handler))
	defer ts.Close()

	client := &http.Client{Timeout: 2 * time.Second}

	var got429 bool
	for i := 0; i < 10; i++ {
		resp, err := client.Post(
			ts.URL+"/v1/messages",
			"application/json",
			strings.NewReader(`{"model":"claude"}`),
		)
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			got429 = true
			break
		}
	}

	if !got429 {
		t.Fatal("expected at least one 429 after exhausting rate limit (3 req/min)")
	}
}
