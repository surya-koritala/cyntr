package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

func setupPolicyHandler(bus *ipc.Bus, decision policy.Decision) {
	bus.Handle("policy", "policy.check", func(msg ipc.Message) (ipc.Message, error) {
		return ipc.Message{
			Type: ipc.MessageTypeResponse,
			Payload: policy.CheckResponse{
				Decision: decision,
				Rule:     "test-rule",
				Reason:   "test",
			},
		}, nil
	})
}

func TestHandlerForwardsAllowedRequest(t *testing.T) {
	// Create upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
	}))
	defer upstream.Close()

	bus := ipc.NewBus()
	defer bus.Close()
	setupPolicyHandler(bus, policy.Allow)

	h := NewHandler(bus, upstream.URL)

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("X-Cyntr-Tenant", "finance")
	req.Header.Set("X-Cyntr-User", "jane@corp.com")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]string
	json.NewDecoder(w.Body).Decode(&body)
	if body["result"] != "ok" {
		t.Fatalf("expected ok, got %v", body)
	}
}

func TestHandlerDeniesBlockedRequest(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	setupPolicyHandler(bus, policy.Deny)

	h := NewHandler(bus, "http://unreachable")

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("X-Cyntr-Tenant", "finance")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestHandlerReturns503WhenPolicyUnavailable(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	// No policy handler registered — fail-closed

	h := NewHandler(bus, "http://unreachable")

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("X-Cyntr-Tenant", "finance")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (fail-closed), got %d", w.Code)
	}
}

func TestHandlerHealthEndpoint(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	h := NewHandler(bus, "http://localhost")

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// Ensure the handler uses a short timeout for policy checks
func TestHandlerPolicyTimeout(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	// Register a slow policy handler
	bus.Handle("policy", "policy.check", func(msg ipc.Message) (ipc.Message, error) {
		time.Sleep(10 * time.Second)
		return ipc.Message{Payload: policy.CheckResponse{Decision: policy.Allow}}, nil
	})

	h := NewHandler(bus, "http://unreachable")
	h.policyTimeout = 100 * time.Millisecond

	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("X-Cyntr-Tenant", "finance")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	// Should fail-closed on timeout
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func init() {
	_ = context.Background // suppress unused import if needed
}
