# Proxy Gateway Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Proxy Gateway — an HTTP reverse proxy that enforces policy on all traffic (model API calls, tool invocations, external agent requests), logs decisions to audit, and supports intent extraction for semantic policy evaluation.

**Architecture:** The Proxy Gateway is a kernel module that starts an HTTP server. Every request flows through: authenticate → extract intent → check policy → forward or deny → log to audit. It supports registering external agent backends (OpenClaw, etc.) and proxying their traffic. Intent extraction uses protocol-specific parsers (Anthropic API, OpenAI-compatible) to extract semantic information (model name, tool calls) from requests for policy evaluation.

**Tech Stack:** Go 1.22+ stdlib `net/http` and `net/http/httputil` (ReverseProxy). No external HTTP framework.

**Spec:** `docs/superpowers/specs/2026-03-19-cyntr-enterprise-platform-design.md` (Section 2.2)

**Dependencies:** Kernel + Policy Engine + Audit Logger + Auth (Plans 1-3).

**Deferred to future plans:**
- WebSocket proxying — this plan handles HTTP only. WebSocket support comes with Plan 6 (Channel Manager).
- gRPC support — future enhancement.
- Rate limiting / circuit breaker — this plan builds the proxy pipeline. Rate limiting is added as middleware later.
- Real upstream forwarding to live LLM APIs — this plan tests with httptest servers.

---

## File Structure

```
modules/proxy/
├── types.go               # ProxyRequest, ProxyResponse, ExternalAgent, Intent
├── parsers/
│   ├── parser.go          # IntentParser interface
│   ├── anthropic.go       # Anthropic API parser
│   └── openai.go          # OpenAI-compatible API parser
├── handler.go             # HTTP handler: auth → intent → policy → forward → audit
├── gateway.go             # Gateway kernel module: starts HTTP server, manages backends
├── types_test.go
├── parsers/
│   ├── anthropic_test.go
│   └── openai_test.go
├── handler_test.go        # Handler pipeline tests with httptest
└── gateway_test.go        # Module IPC + HTTP integration tests
```

---

## Chunk 1: Types + Intent Parsers

### Task 1: Define Proxy Types

**Files:**
- Create: `modules/proxy/types.go`
- Create: `modules/proxy/types_test.go`

- [ ] **Step 1: Write failing test**

Create `modules/proxy/types_test.go`:
```go
package proxy

import "testing"

func TestIntentString(t *testing.T) {
	i := Intent{
		Action:    "model_call",
		Provider:  "anthropic",
		Model:     "claude-sonnet-4-20250514",
		Tool:      "",
	}
	if i.Action != "model_call" {
		t.Fatalf("expected model_call, got %q", i.Action)
	}
}

func TestExternalAgentKey(t *testing.T) {
	ea := ExternalAgent{
		Name:     "marketing-openclaw",
		Tenant:   "marketing",
		Type:     "openclaw",
		Endpoint: "http://localhost:18789",
	}
	if ea.Key() != "marketing/marketing-openclaw" {
		t.Fatalf("expected marketing/marketing-openclaw, got %q", ea.Key())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/proxy/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement types**

Create `modules/proxy/types.go`:
```go
package proxy

// Intent represents the semantic meaning extracted from an HTTP request.
type Intent struct {
	Action   string // "model_call", "tool_call", "unknown"
	Provider string // "anthropic", "openai", ""
	Model    string // model name if detected
	Tool     string // tool name if detected
}

// ExternalAgent represents a registered external agent backend.
type ExternalAgent struct {
	Name     string `yaml:"name"`
	Tenant   string `yaml:"tenant"`
	Type     string `yaml:"type"`     // "openclaw", "langchain", etc.
	Endpoint string `yaml:"endpoint"` // upstream URL
}

// Key returns the unique identifier for this external agent.
func (ea ExternalAgent) Key() string {
	return ea.Tenant + "/" + ea.Name
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/proxy/ -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add modules/proxy/types.go modules/proxy/types_test.go
git commit -m "feat(proxy): define Proxy Gateway types — Intent, ExternalAgent"
```

---

### Task 2: Implement Intent Parsers

**Files:**
- Create: `modules/proxy/parsers/parser.go`
- Create: `modules/proxy/parsers/anthropic.go`
- Create: `modules/proxy/parsers/openai.go`
- Create: `modules/proxy/parsers/anthropic_test.go`
- Create: `modules/proxy/parsers/openai_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/proxy/parsers/anthropic_test.go`:
```go
package parsers

import (
	"net/http"
	"strings"
	"testing"
)

func TestAnthropicParserDetectsModelCall(t *testing.T) {
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hello"}]}`
	req, _ := http.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "sk-test")

	p := &AnthropicParser{}
	intent, err := p.Parse(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if intent.Action != "model_call" {
		t.Fatalf("expected model_call, got %q", intent.Action)
	}
	if intent.Provider != "anthropic" {
		t.Fatalf("expected anthropic, got %q", intent.Provider)
	}
	if intent.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("expected claude-sonnet-4-20250514, got %q", intent.Model)
	}
}

func TestAnthropicParserDetectsToolUse(t *testing.T) {
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Run ls"}],"tools":[{"name":"shell_exec"}]}`
	req, _ := http.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	p := &AnthropicParser{}
	intent, _ := p.Parse(req)
	if intent.Tool != "shell_exec" {
		t.Fatalf("expected shell_exec tool, got %q", intent.Tool)
	}
}

func TestAnthropicParserNoMatch(t *testing.T) {
	req, _ := http.NewRequest("GET", "/health", nil)
	p := &AnthropicParser{}
	ok := p.Matches(req)
	if ok {
		t.Fatal("expected no match for GET /health")
	}
}

func TestAnthropicParserMatches(t *testing.T) {
	req, _ := http.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("X-API-Key", "sk-test")
	p := &AnthropicParser{}
	if !p.Matches(req) {
		t.Fatal("expected match for POST /v1/messages with X-API-Key")
	}
}
```

Create `modules/proxy/parsers/openai_test.go`:
```go
package parsers

import (
	"net/http"
	"strings"
	"testing"
)

func TestOpenAIParserDetectsModelCall(t *testing.T) {
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}`
	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-test")

	p := &OpenAIParser{}
	intent, err := p.Parse(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if intent.Action != "model_call" {
		t.Fatalf("expected model_call, got %q", intent.Action)
	}
	if intent.Provider != "openai" {
		t.Fatalf("expected openai, got %q", intent.Provider)
	}
	if intent.Model != "gpt-4" {
		t.Fatalf("expected gpt-4, got %q", intent.Model)
	}
}

func TestOpenAIParserDetectsToolUse(t *testing.T) {
	body := `{"model":"gpt-4","messages":[],"tools":[{"type":"function","function":{"name":"get_weather"}}]}`
	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	p := &OpenAIParser{}
	intent, _ := p.Parse(req)
	if intent.Tool != "get_weather" {
		t.Fatalf("expected get_weather, got %q", intent.Tool)
	}
}

func TestOpenAIParserMatches(t *testing.T) {
	req, _ := http.NewRequest("POST", "/v1/chat/completions", nil)
	p := &OpenAIParser{}
	if !p.Matches(req) {
		t.Fatal("expected match")
	}
}

func TestOpenAIParserNoMatch(t *testing.T) {
	req, _ := http.NewRequest("GET", "/health", nil)
	p := &OpenAIParser{}
	if p.Matches(req) {
		t.Fatal("expected no match")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/proxy/parsers/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement parser interface and parsers**

Create `modules/proxy/parsers/parser.go`:
```go
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
```

Create `modules/proxy/parsers/anthropic.go`:
```go
package parsers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/proxy"
)

// AnthropicParser extracts intent from Anthropic API requests.
type AnthropicParser struct{}

func (p *AnthropicParser) Matches(r *http.Request) bool {
	return r.Method == "POST" &&
		strings.HasPrefix(r.URL.Path, "/v1/messages") &&
		r.Header.Get("X-API-Key") != ""
}

func (p *AnthropicParser) Parse(r *http.Request) (proxy.Intent, error) {
	intent := proxy.Intent{
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
```

Create `modules/proxy/parsers/openai.go`:
```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/proxy/... -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/proxy/parsers/
git commit -m "feat(proxy): implement intent parsers for Anthropic and OpenAI APIs"
```

---

## Chunk 2: HTTP Handler Pipeline

### Task 3: Implement Proxy Handler

**Files:**
- Create: `modules/proxy/handler.go`
- Create: `modules/proxy/handler_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/proxy/handler_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/proxy/ -run TestHandler -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement handler**

Create `modules/proxy/handler.go`:
```go
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
			intent, err := p.Parse(r)
			if err == nil {
				return intent
			}
		}
	}
	// Fallback: unknown intent
	return Intent{
		Action: "unknown",
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/proxy/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/proxy/handler.go modules/proxy/handler_test.go
git commit -m "feat(proxy): implement HTTP handler with policy enforcement and intent extraction"
```

---

## Chunk 3: Gateway Module + Integration Test

### Task 4: Implement Gateway as Kernel Module

**Files:**
- Create: `modules/proxy/gateway.go`
- Create: `modules/proxy/gateway_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/proxy/gateway_test.go`:
```go
package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

func TestGatewayImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Gateway)(nil)
}

func TestGatewayStartsHTTPServer(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	setupPolicyHandler(bus, policy.Allow)

	gw := NewGateway("127.0.0.1:0") // port 0 = random available port
	ctx := context.Background()
	gw.Init(ctx, &kernel.Services{Bus: bus})
	gw.Start(ctx)
	defer gw.Stop(ctx)

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	addr := gw.Addr()
	if addr == "" {
		t.Fatal("expected server address")
	}

	// Hit health endpoint
	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestGatewayRegisterExternalAgent(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	gw := NewGateway("127.0.0.1:0")
	ctx := context.Background()
	gw.Init(ctx, &kernel.Services{Bus: bus})
	gw.Start(ctx)
	defer gw.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "proxy", Topic: "proxy.register",
		Payload: ExternalAgent{
			Name: "marketing-openclaw", Tenant: "marketing",
			Type: "openclaw", Endpoint: "http://localhost:18789",
		},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("expected ok, got %v", resp.Payload)
	}
}

func TestGatewayListExternalAgents(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	gw := NewGateway("127.0.0.1:0")
	ctx := context.Background()
	gw.Init(ctx, &kernel.Services{Bus: bus})
	gw.Start(ctx)
	defer gw.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Register an agent
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "proxy", Topic: "proxy.register",
		Payload: ExternalAgent{Name: "test-agent", Tenant: "finance", Type: "openclaw", Endpoint: "http://localhost:1234"},
	})

	// List
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "proxy", Topic: "proxy.list",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	agents, ok := resp.Payload.([]ExternalAgent)
	if !ok {
		t.Fatalf("expected []ExternalAgent, got %T", resp.Payload)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1, got %d", len(agents))
	}
}

func TestGatewayHealthy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	gw := NewGateway("127.0.0.1:0")
	ctx := context.Background()
	gw.Init(ctx, &kernel.Services{Bus: bus})
	gw.Start(ctx)
	defer gw.Stop(ctx)

	time.Sleep(100 * time.Millisecond)
	health := gw.Health(ctx)
	if !health.Healthy {
		t.Fatalf("expected healthy: %s", health.Message)
	}
}

func TestGatewayDeniesWithoutPolicy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	// No policy handler — fail-closed

	gw := NewGateway("127.0.0.1:0")
	ctx := context.Background()
	gw.Init(ctx, &kernel.Services{Bus: bus})
	gw.Start(ctx)
	defer gw.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Post(
		"http://"+gw.Addr()+"/v1/messages",
		"application/json",
		strings.NewReader(`{"model":"claude"}`),
	)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "policy engine unavailable" {
		t.Fatalf("expected policy error, got %v", body)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/proxy/ -run TestGateway -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement gateway module**

Create `modules/proxy/gateway.go`:
```go
package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Gateway is the Proxy Gateway kernel module.
type Gateway struct {
	listenAddr string
	bus        *ipc.Bus
	server     *http.Server
	listener   net.Listener
	mu         sync.RWMutex
	agents     map[string]ExternalAgent
}

// NewGateway creates a new Proxy Gateway module.
func NewGateway(listenAddr string) *Gateway {
	return &Gateway{
		listenAddr: listenAddr,
		agents:     make(map[string]ExternalAgent),
	}
}

func (g *Gateway) Name() string           { return "proxy" }
func (g *Gateway) Dependencies() []string { return []string{"policy"} }

func (g *Gateway) Init(ctx context.Context, svc *kernel.Services) error {
	g.bus = svc.Bus
	return nil
}

func (g *Gateway) Start(ctx context.Context) error {
	// Register IPC handlers
	g.bus.Handle("proxy", "proxy.register", g.handleRegister)
	g.bus.Handle("proxy", "proxy.list", g.handleList)

	// Create HTTP handler
	handler := NewHandler(g.bus, "") // no default upstream — uses registered agents

	// Start HTTP server
	ln, err := net.Listen("tcp", g.listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	g.listener = ln

	g.server = &http.Server{Handler: handler}

	go g.server.Serve(ln)

	return nil
}

func (g *Gateway) Stop(ctx context.Context) error {
	if g.server != nil {
		return g.server.Shutdown(ctx)
	}
	return nil
}

func (g *Gateway) Health(ctx context.Context) kernel.HealthStatus {
	if g.listener == nil {
		return kernel.HealthStatus{Healthy: false, Message: "not listening"}
	}
	return kernel.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("listening on %s", g.listener.Addr().String()),
	}
}

// Addr returns the actual address the server is listening on.
func (g *Gateway) Addr() string {
	if g.listener == nil {
		return ""
	}
	return g.listener.Addr().String()
}

func (g *Gateway) handleRegister(msg ipc.Message) (ipc.Message, error) {
	agent, ok := msg.Payload.(ExternalAgent)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected ExternalAgent, got %T", msg.Payload)
	}

	g.mu.Lock()
	g.agents[agent.Key()] = agent
	g.mu.Unlock()

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (g *Gateway) handleList(msg ipc.Message) (ipc.Message, error) {
	g.mu.RLock()
	agents := make([]ExternalAgent, 0, len(g.agents))
	for _, a := range g.agents {
		agents = append(agents, a)
	}
	g.mu.RUnlock()

	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Key() < agents[j].Key()
	})

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: agents}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/proxy/... -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/proxy/gateway.go modules/proxy/gateway_test.go
git commit -m "feat(proxy): implement Proxy Gateway kernel module with HTTP server and agent registration"
```

---

### Task 5: Integration Test

**Files:**
- Create: `tests/integration/proxy_test.go`

- [ ] **Step 1: Write integration test**

Create `tests/integration/proxy_test.go`:
```go
package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/modules/proxy"
)

func TestProxyGatewayPolicyEnforcement(t *testing.T) {
	dir := t.TempDir()

	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte(`
rules:
  - name: deny-shell
    tenant: finance
    action: "*"
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 20
  - name: allow-all
    tenant: "*"
    action: "*"
    tool: "*"
    agent: "*"
    decision: allow
    priority: 1
`), 0644)

	cfgPath := filepath.Join(dir, "cyntr.yaml")
	os.WriteFile(cfgPath, []byte("version: \"1\"\nlisten:\n  address: \"127.0.0.1:8080\"\n"), 0644)

	k := kernel.New()
	if err := k.LoadConfig(cfgPath); err != nil {
		t.Fatalf("config: %v", err)
	}

	policyEngine := policy.NewEngine(policyPath)
	gateway := proxy.NewGateway("127.0.0.1:0")

	if err := k.Register(policyEngine); err != nil {
		t.Fatalf("register policy: %v", err)
	}
	if err := k.Register(gateway); err != nil {
		t.Fatalf("register proxy: %v", err)
	}

	ctx := t.Context()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer k.Stop(ctx)

	time.Sleep(200 * time.Millisecond)

	addr := gateway.Addr()

	// Test 1: Allowed request (marketing tenant, model call)
	req, _ := http.NewRequest("POST", "http://"+addr+"/v1/messages",
		strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("X-Cyntr-Tenant", "marketing")
	req.Header.Set("Content-Type", "application/json")

	// Note: this will fail to proxy (no upstream), but it should get past policy
	// The upstream failure gives us a 502, not a 403
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	// 502 means policy allowed but upstream failed (expected — no real upstream)
	if resp.StatusCode == http.StatusForbidden {
		t.Fatal("marketing should not be denied")
	}

	// Test 2: Health endpoint
	resp, err = http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var health map[string]string
	json.NewDecoder(resp.Body).Decode(&health)
	if health["status"] != "ok" {
		t.Fatalf("expected ok, got %v", health)
	}
}
```

- [ ] **Step 2: Run integration test**

Run: `cd /Users/suryakoritala/Cyntr && go test ./tests/integration/ -run TestProxy -v -count=1 -race`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add tests/integration/proxy_test.go
git commit -m "feat: add integration test — proxy gateway with policy enforcement"
```

---

### Task 6: Final Verification

- [ ] **Step 1: Run complete test suite**

Run: `cd /Users/suryakoritala/Cyntr && go test ./... -count=1 -race`
Expected: All PASS

- [ ] **Step 2: Run go vet**

Run: `cd /Users/suryakoritala/Cyntr && go vet ./...`

- [ ] **Step 3: Build binary**

Run: `cd /Users/suryakoritala/Cyntr && go build -o cyntr ./cmd/cyntr && ./cyntr version`
Expected: `cyntr v0.1.0`
