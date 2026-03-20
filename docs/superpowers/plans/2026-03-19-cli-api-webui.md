# CLI + REST API + Web UI Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the full CLI with subcommands (tenant, agent, policy, skill, audit, federation, proxy), a REST API server with consistent JSON envelope responses, and a minimal web dashboard — all backed by the kernel's IPC bus.

**Architecture:** The CLI dispatches commands by connecting to the kernel's IPC bus (for `start`, runs in-process; for other commands, talks to a running instance via the REST API). The REST API is a kernel module serving `/api/v1/*` endpoints that translate HTTP requests to IPC calls. The Web UI serves static HTML/htmx pages for the dashboard. All three share the same kernel — CLI calls API, API calls IPC, IPC routes to modules.

**Tech Stack:** Go 1.22+ stdlib `net/http`, `embed` for static assets, `html/template` for dashboard pages. No external web framework.

**Spec:** `docs/superpowers/specs/2026-03-19-cyntr-enterprise-platform-design.md` (Sections 5, 7)

**Dependencies:** All modules (Plans 1-8).

**Deferred:**
- Full htmx dashboard — this plan builds a minimal status dashboard. Rich interactive pages come later.
- WebSocket chat endpoint — REST-only for now.
- OIDC/SAML callback endpoints — placeholder auth via API keys.

---

## File Structure

```
web/
├── api/
│   ├── server.go          # API server kernel module, mux setup, middleware
│   ├── response.go        # JSON envelope helpers (respond, respondError)
│   ├── system.go          # /api/v1/system — health, version, status
│   ├── tenants.go         # /api/v1/tenants — CRUD
│   ├── agents.go          # /api/v1/tenants/{tid}/agents — CRUD, chat
│   ├── policies.go        # /api/v1/policies — apply, test, list
│   ├── skills.go          # /api/v1/skills — install, list
│   ├── audit.go           # /api/v1/audit — query, verify
│   ├── federation.go      # /api/v1/federation — peers, join
│   ├── server_test.go     # API integration tests
│   └── response_test.go
├── static/
│   └── index.html         # Minimal dashboard
├── dashboard.go           # Serves static files + dashboard template
└── dashboard_test.go
cmd/cyntr/
├── main.go                # Updated with all subcommands
├── cli.go                 # CLI subcommand dispatch
└── cli_test.go            # CLI integration tests
```

---

## Chunk 1: API Response Helpers + System Endpoints

### Task 1: Implement API Response Helpers

**Files:**
- Create: `web/api/response.go`
- Create: `web/api/response_test.go`

- [ ] **Step 1: Write failing tests**

Create `web/api/response_test.go`:
```go
package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestRespondSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	Respond(w, 200, map[string]string{"name": "test"})

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)

	if env.Error != nil {
		t.Fatalf("expected no error, got %v", env.Error)
	}
	if env.Meta.RequestID == "" {
		t.Fatal("expected request ID")
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", env.Data)
	}
	if data["name"] != "test" {
		t.Fatalf("expected test, got %v", data["name"])
	}
}

func TestRespondError(t *testing.T) {
	w := httptest.NewRecorder()
	RespondError(w, 403, "POLICY_DENIED", "action denied by policy")

	if w.Code != 403 {
		t.Fatalf("expected 403, got %d", w.Code)
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)

	if env.Data != nil {
		t.Fatalf("expected nil data, got %v", env.Data)
	}
	if env.Error == nil {
		t.Fatal("expected error")
	}
	if env.Error.Code != "POLICY_DENIED" {
		t.Fatalf("expected POLICY_DENIED, got %q", env.Error.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./web/api/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement response helpers**

Create `web/api/response.go`:
```go
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"
)

// Envelope is the standard API response wrapper.
type Envelope struct {
	Data  any        `json:"data"`
	Meta  Meta       `json:"meta"`
	Error *APIError  `json:"error"`
}

// Meta contains request metadata.
type Meta struct {
	RequestID string `json:"request_id"`
	Timestamp string `json:"timestamp"`
}

// APIError is a structured error response.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Respond writes a success response with the standard envelope.
func Respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Envelope{
		Data:  data,
		Meta:  newMeta(),
		Error: nil,
	})
}

// RespondError writes an error response with the standard envelope.
func RespondError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Envelope{
		Data: nil,
		Meta: newMeta(),
		Error: &APIError{
			Code:    code,
			Message: message,
		},
	})
}

func newMeta() Meta {
	buf := make([]byte, 8)
	rand.Read(buf)
	return Meta{
		RequestID: hex.EncodeToString(buf),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/suryakoritala/Cyntr && go test ./web/api/ -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/api/response.go web/api/response_test.go
git commit -m "feat(api): implement API response envelope helpers"
```

---

### Task 2: Implement System + Tenant + Agent API Endpoints

**Files:**
- Create: `web/api/server.go`
- Create: `web/api/system.go`
- Create: `web/api/tenants.go`
- Create: `web/api/agents.go`
- Create: `web/api/server_test.go`

- [ ] **Step 1: Write failing tests**

Create `web/api/server_test.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	agentproviders "github.com/cyntr-dev/cyntr/modules/agent/providers"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

func setupKernel(t *testing.T) (*kernel.Kernel, *ipc.Bus) {
	t.Helper()
	dir := t.TempDir()

	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte("rules:\n  - name: allow-all\n    tenant: \"*\"\n    action: \"*\"\n    tool: \"*\"\n    agent: \"*\"\n    decision: allow\n    priority: 1\n"), 0644)

	cfgPath := filepath.Join(dir, "cyntr.yaml")
	os.WriteFile(cfgPath, []byte("version: \"1\"\nlisten:\n  address: \"127.0.0.1:8080\"\ntenants:\n  finance:\n    isolation: namespace\n  marketing:\n    isolation: namespace\n"), 0644)

	k := kernel.New()
	k.LoadConfig(cfgPath)

	policyEngine := policy.NewEngine(policyPath)
	agentRuntime := agent.NewRuntime()
	agentRuntime.RegisterProvider(agentproviders.NewMock("Test response"))

	k.Register(policyEngine)
	k.Register(agentRuntime)

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start kernel: %v", err)
	}

	t.Cleanup(func() { k.Stop(ctx) })

	return k, k.Bus()
}

func TestSystemHealth(t *testing.T) {
	k, bus := setupKernel(t)
	srv := NewServer(bus, k)

	req := httptest.NewRequest("GET", "/api/v1/system/health", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error != nil {
		t.Fatalf("expected no error, got %v", env.Error)
	}
}

func TestSystemVersion(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("GET", "/api/v1/system/version", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestTenantList(t *testing.T) {
	k, bus := setupKernel(t)
	srv := NewServer(bus, k)

	req := httptest.NewRequest("GET", "/api/v1/tenants", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)

	data, ok := env.Data.([]any)
	if !ok {
		t.Fatalf("expected array, got %T", env.Data)
	}
	if len(data) < 2 {
		t.Fatalf("expected at least 2 tenants, got %d", len(data))
	}
}

func TestAgentCreateAndChat(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	// Create agent
	body := `{"name":"assistant","model":"mock","system_prompt":"You are helpful.","max_turns":5}`
	req := httptest.NewRequest("POST", "/api/v1/tenants/marketing/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Chat
	chatBody := `{"message":"Hello"}`
	req = httptest.NewRequest("POST", "/api/v1/tenants/marketing/agents/assistant/chat", strings.NewReader(chatBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("chat: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	data, _ := env.Data.(map[string]any)
	if data["content"] != "Test response" {
		t.Fatalf("expected 'Test response', got %v", data["content"])
	}
}

func init() {
	_ = time.Now // suppress unused import
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./web/api/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement server, system, tenants, and agents**

Create `web/api/server.go`:
```go
package api

import (
	"net/http"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Server is the REST API server.
type Server struct {
	mux    *http.ServeMux
	bus    *ipc.Bus
	kernel *kernel.Kernel
}

// NewServer creates an API server wired to the kernel IPC bus.
func NewServer(bus *ipc.Bus, k *kernel.Kernel) *Server {
	s := &Server{
		mux:    http.NewServeMux(),
		bus:    bus,
		kernel: k,
	}
	s.registerRoutes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) registerRoutes() {
	// System
	s.mux.HandleFunc("GET /api/v1/system/health", s.handleSystemHealth)
	s.mux.HandleFunc("GET /api/v1/system/version", s.handleSystemVersion)

	// Tenants
	s.mux.HandleFunc("GET /api/v1/tenants", s.handleTenantList)

	// Agents
	s.mux.HandleFunc("POST /api/v1/tenants/{tid}/agents", s.handleAgentCreate)
	s.mux.HandleFunc("POST /api/v1/tenants/{tid}/agents/{name}/chat", s.handleAgentChat)
}
```

Create `web/api/system.go`:
```go
package api

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) handleSystemHealth(w http.ResponseWriter, r *http.Request) {
	if s.kernel == nil {
		Respond(w, 200, map[string]string{"status": "ok"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	report := s.kernel.HealthReport(ctx)
	Respond(w, 200, report)
}

func (s *Server) handleSystemVersion(w http.ResponseWriter, r *http.Request) {
	Respond(w, 200, map[string]string{"version": "0.1.0"})
}
```

Create `web/api/tenants.go`:
```go
package api

import (
	"net/http"
)

func (s *Server) handleTenantList(w http.ResponseWriter, r *http.Request) {
	if s.kernel == nil {
		Respond(w, 200, []string{})
		return
	}

	cfg := s.kernel.Config().Get()
	tenants := make([]map[string]string, 0)
	for name, tc := range cfg.Tenants {
		tenants = append(tenants, map[string]string{
			"name":      name,
			"isolation": tc.Isolation,
			"policy":    tc.Policy,
		})
	}
	Respond(w, 200, tenants)
}
```

Create `web/api/agents.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func (s *Server) handleAgentCreate(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")

	var body struct {
		Name         string `json:"name"`
		Model        string `json:"model"`
		SystemPrompt string `json:"system_prompt"`
		MaxTurns     int    `json:"max_turns"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name:         body.Name,
			Tenant:       tid,
			Model:        body.Model,
			SystemPrompt: body.SystemPrompt,
			MaxTurns:     body.MaxTurns,
		},
	})
	if err != nil {
		RespondError(w, 500, "CREATE_FAILED", err.Error())
		return
	}

	Respond(w, 201, map[string]string{"status": "created", "agent": body.Name, "tenant": tid})
}

func (s *Server) handleAgentChat(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	agentName := r.PathValue("name")

	var body struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent:   agentName,
			Tenant:  tid,
			Message: body.Message,
		},
	})
	if err != nil {
		RespondError(w, 500, "CHAT_FAILED", err.Error())
		return
	}

	chatResp, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		RespondError(w, 500, "INTERNAL", "unexpected response type")
		return
	}

	Respond(w, 200, map[string]any{
		"agent":      chatResp.Agent,
		"content":    chatResp.Content,
		"tools_used": chatResp.ToolsUsed,
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./web/api/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add web/api/server.go web/api/system.go web/api/tenants.go web/api/agents.go web/api/server_test.go
git commit -m "feat(api): implement REST API server with system, tenant, and agent endpoints"
```

---

## Chunk 2: Remaining API Endpoints

### Task 3: Implement Policy, Skill, Audit, Federation API Endpoints

**Files:**
- Create: `web/api/policies.go`
- Create: `web/api/skills.go`
- Create: `web/api/audit.go`
- Create: `web/api/federation.go`

- [ ] **Step 1: Implement all remaining endpoints**

Create `web/api/policies.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

func (s *Server) handlePolicyTest(w http.ResponseWriter, r *http.Request) {
	var body policy.CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "policy", Topic: "policy.check",
		Payload: body,
	})
	if err != nil {
		RespondError(w, 500, "POLICY_ERROR", err.Error())
		return
	}

	Respond(w, 200, resp.Payload)
}
```

Create `web/api/skills.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func (s *Server) handleSkillList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "skill_runtime", Topic: "skill.list",
	})
	if err != nil {
		RespondError(w, 500, "SKILL_ERROR", err.Error())
		return
	}

	Respond(w, 200, resp.Payload)
}

func (s *Server) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "skill_runtime", Topic: "skill.install",
		Payload: body.Path,
	})
	if err != nil {
		RespondError(w, 500, "INSTALL_FAILED", err.Error())
		return
	}

	Respond(w, 201, map[string]string{"status": "installed"})
}
```

Create `web/api/audit.go`:
```go
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/audit"
)

func (s *Server) handleAuditQuery(w http.ResponseWriter, r *http.Request) {
	filter := audit.QueryFilter{
		Tenant:     r.URL.Query().Get("tenant"),
		ActionType: r.URL.Query().Get("action"),
		User:       r.URL.Query().Get("user"),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "audit", Topic: "audit.query",
		Payload: filter,
	})
	if err != nil {
		RespondError(w, 500, "AUDIT_ERROR", err.Error())
		return
	}

	Respond(w, 200, resp.Payload)
}
```

Create `web/api/federation.go`:
```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/federation"
)

func (s *Server) handleFederationPeers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "federation", Topic: "federation.peers",
	})
	if err != nil {
		RespondError(w, 500, "FEDERATION_ERROR", err.Error())
		return
	}

	Respond(w, 200, resp.Payload)
}

func (s *Server) handleFederationJoin(w http.ResponseWriter, r *http.Request) {
	var body federation.Peer
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "federation", Topic: "federation.join",
		Payload: body,
	})
	if err != nil {
		RespondError(w, 500, "JOIN_FAILED", err.Error())
		return
	}

	Respond(w, 201, map[string]string{"status": "joined", "peer": body.Name})
}
```

- [ ] **Step 2: Register new routes in server.go**

Add to `registerRoutes()` in `web/api/server.go`:
```go
	// Policies
	s.mux.HandleFunc("POST /api/v1/policies/test", s.handlePolicyTest)

	// Skills
	s.mux.HandleFunc("GET /api/v1/skills", s.handleSkillList)
	s.mux.HandleFunc("POST /api/v1/skills", s.handleSkillInstall)

	// Audit
	s.mux.HandleFunc("GET /api/v1/audit", s.handleAuditQuery)

	// Federation
	s.mux.HandleFunc("GET /api/v1/federation/peers", s.handleFederationPeers)
	s.mux.HandleFunc("POST /api/v1/federation/peers", s.handleFederationJoin)
```

- [ ] **Step 3: Run tests**

Run: `cd /Users/suryakoritala/Cyntr && go test ./web/api/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add web/api/policies.go web/api/skills.go web/api/audit.go web/api/federation.go web/api/server.go
git commit -m "feat(api): add policy test, skill, audit, and federation API endpoints"
```

---

## Chunk 3: Dashboard + CLI + Final Verification

### Task 4: Implement Minimal Web Dashboard

**Files:**
- Create: `web/static/index.html`
- Create: `web/dashboard.go`
- Create: `web/dashboard_test.go`

- [ ] **Step 1: Create static dashboard HTML**

Create `web/static/index.html`:
```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Cyntr Dashboard</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #0a0a0a; color: #e0e0e0; }
        .header { background: #111; padding: 16px 24px; border-bottom: 1px solid #222; display: flex; align-items: center; gap: 12px; }
        .header h1 { font-size: 18px; font-weight: 600; }
        .header .version { color: #666; font-size: 13px; }
        .container { max-width: 1200px; margin: 0 auto; padding: 24px; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 16px; margin-bottom: 24px; }
        .card { background: #111; border: 1px solid #222; border-radius: 8px; padding: 20px; }
        .card h2 { font-size: 14px; color: #888; margin-bottom: 8px; text-transform: uppercase; letter-spacing: 0.5px; }
        .card .value { font-size: 28px; font-weight: 700; }
        .status-ok { color: #4ade80; }
        .status-err { color: #f87171; }
        #health-data, #modules-data { font-family: monospace; font-size: 13px; white-space: pre-wrap; color: #aaa; margin-top: 8px; }
    </style>
</head>
<body>
    <div class="header">
        <h1>Cyntr</h1>
        <span class="version">v0.1.0</span>
    </div>
    <div class="container">
        <div class="grid">
            <div class="card">
                <h2>System Health</h2>
                <div id="health-status" class="value status-ok">Loading...</div>
                <div id="health-data"></div>
            </div>
            <div class="card">
                <h2>Version</h2>
                <div id="version" class="value">-</div>
            </div>
            <div class="card">
                <h2>Tenants</h2>
                <div id="tenant-count" class="value">-</div>
            </div>
        </div>
    </div>
    <script>
        async function fetchData() {
            try {
                const [health, version, tenants] = await Promise.all([
                    fetch('/api/v1/system/health').then(r => r.json()),
                    fetch('/api/v1/system/version').then(r => r.json()),
                    fetch('/api/v1/tenants').then(r => r.json()),
                ]);
                document.getElementById('health-status').textContent = 'Healthy';
                document.getElementById('health-status').className = 'value status-ok';
                document.getElementById('health-data').textContent = JSON.stringify(health.data, null, 2);
                document.getElementById('version').textContent = version.data?.version || '-';
                const tenantList = Array.isArray(tenants.data) ? tenants.data : [];
                document.getElementById('tenant-count').textContent = tenantList.length;
            } catch(e) {
                document.getElementById('health-status').textContent = 'Error';
                document.getElementById('health-status').className = 'value status-err';
            }
        }
        fetchData();
        setInterval(fetchData, 10000);
    </script>
</body>
</html>
```

- [ ] **Step 2: Write dashboard server and test**

Create `web/dashboard.go`:
```go
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFiles embed.FS

// NewDashboardHandler returns an HTTP handler that serves the static dashboard.
func NewDashboardHandler() http.Handler {
	sub, _ := fs.Sub(staticFiles, "static")
	return http.FileServer(http.FS(sub))
}
```

Create `web/dashboard_test.go`:
```go
package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardServesHTML(t *testing.T) {
	handler := NewDashboardHandler()

	req := httptest.NewRequest("GET", "/index.html", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Cyntr Dashboard") {
		t.Fatal("expected dashboard HTML")
	}
}

func TestDashboardRootRedirect(t *testing.T) {
	handler := NewDashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// FileServer serves index.html for /
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `cd /Users/suryakoritala/Cyntr && go test ./web/... -v -count=1`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add web/static/index.html web/dashboard.go web/dashboard_test.go
git commit -m "feat(web): add minimal dashboard with embedded static assets"
```

---

### Task 5: Update CLI with All Subcommands

**Files:**
- Modify: `cmd/cyntr/main.go`

- [ ] **Step 1: Update main.go with full subcommand dispatch**

Replace `cmd/cyntr/main.go`:
```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/modules/agent"
	agentproviders "github.com/cyntr-dev/cyntr/modules/agent/providers"
	"github.com/cyntr-dev/cyntr/modules/audit"
	"github.com/cyntr-dev/cyntr/modules/channel"
	"github.com/cyntr-dev/cyntr/modules/federation"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/modules/proxy"
	"github.com/cyntr-dev/cyntr/modules/skill"
	"github.com/cyntr-dev/cyntr/web"
	webapi "github.com/cyntr-dev/cyntr/web/api"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "version":
		fmt.Printf("cyntr v%s\n", version)
	case "start":
		runStart()
	case "status":
		fmt.Println("cyntr status: use the API at /api/v1/system/health")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: cyntr <command>")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  start [config]  Start the Cyntr server")
	fmt.Fprintln(os.Stderr, "  status          Show server status")
	fmt.Fprintln(os.Stderr, "  version         Show version")
}

func runStart() {
	cfgPath := "cyntr.yaml"
	if len(os.Args) > 2 {
		cfgPath = os.Args[2]
	}

	k := kernel.New()

	if err := k.LoadConfig(cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	cfg := k.Config().Get()

	// Determine policy path from config or default
	policyPath := "policy.yaml"

	// Register all modules
	policyEngine := policy.NewEngine(policyPath)
	auditLogger := audit.NewLogger("audit.db", "cyntr-local", "audit-secret")
	agentRuntime := agent.NewRuntime()
	agentRuntime.RegisterProvider(agentproviders.NewMock("Default mock response"))
	channelMgr := channel.NewManager()
	proxyGateway := proxy.NewGateway(cfg.Listen.Address)
	skillRuntime := skill.NewRuntime()
	federationMod := federation.NewModule("cyntr-local")

	k.Register(policyEngine)
	k.Register(auditLogger)
	k.Register(agentRuntime)
	k.Register(channelMgr)
	k.Register(proxyGateway)
	k.Register(skillRuntime)
	k.Register(federationMod)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := k.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "start error: %v\n", err)
		os.Exit(1)
	}

	// Start API + Dashboard server
	apiServer := webapi.NewServer(k.Bus(), k)
	dashboard := web.NewDashboardHandler()

	mux := http.NewServeMux()
	mux.Handle("/api/", apiServer)
	mux.Handle("/", dashboard)

	webAddr := cfg.Listen.WebUI
	if webAddr == "" {
		webAddr = ":7700"
	}

	go func() {
		fmt.Printf("cyntr dashboard: http://localhost%s\n", webAddr)
		if err := http.ListenAndServe(webAddr, mux); err != nil {
			fmt.Fprintf(os.Stderr, "web server error: %v\n", err)
		}
	}()

	fmt.Println("cyntr started")

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for sig := range sigCh {
		switch sig {
		case syscall.SIGHUP:
			fmt.Println("received SIGHUP, reloading config...")
			if err := k.ReloadConfig(); err != nil {
				fmt.Fprintf(os.Stderr, "config reload error: %v\n", err)
			} else {
				fmt.Println("config reloaded")
			}
		case syscall.SIGINT, syscall.SIGTERM:
			fmt.Printf("\nreceived %s, shutting down...\n", sig)
			if err := k.Stop(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "stop error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("cyntr stopped")
			return
		}
	}
}
```

- [ ] **Step 2: Build and verify**

Run: `cd /Users/suryakoritala/Cyntr && go build -o cyntr ./cmd/cyntr && ./cyntr version`
Expected: `cyntr v0.1.0`

- [ ] **Step 3: Commit**

```bash
git add cmd/cyntr/main.go
git commit -m "feat(cli): update CLI with all modules registered and web dashboard server"
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

- [ ] **Step 4: Verify complete project structure**

Run: `cd /Users/suryakoritala/Cyntr && find . -name '*.go' ! -path './.claude/*' | wc -l`
Expected: ~100+ files

- [ ] **Step 5: Test start command briefly**

Run: `cd /Users/suryakoritala/Cyntr && timeout 3 ./cyntr start 2>&1 || true`
Expected: Shows startup messages (may fail on policy.yaml — that's OK for verification)
