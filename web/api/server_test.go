package api

import (
	"bufio"
	"bytes"
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
	"github.com/cyntr-dev/cyntr/modules/audit"
	"github.com/cyntr-dev/cyntr/modules/federation"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/modules/skill"
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

	auditDBPath := filepath.Join(dir, "audit.db")
	auditLogger := audit.NewLogger(auditDBPath, "test-node", "test-secret")

	skillRuntime := skill.NewRuntime()

	fedModule := federation.NewModule("test-node")

	k.Register(policyEngine)
	k.Register(agentRuntime)
	k.Register(auditLogger)
	k.Register(skillRuntime)
	k.Register(fedModule)

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start kernel: %v", err)
	}

	t.Cleanup(func() { k.Stop(ctx) })

	return k, k.Bus()
}

// ---------------------------------------------------------------------------
// System
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Tenants
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Agents
// ---------------------------------------------------------------------------

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

func TestAgentCreate_BadJSON(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("POST", "/api/v1/tenants/marketing/agents", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error == nil || env.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("expected INVALID_REQUEST error, got %v", env.Error)
	}
}

func TestAgentChat_NonexistentAgent(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	chatBody := `{"message":"Hello"}`
	req := httptest.NewRequest("POST", "/api/v1/tenants/marketing/agents/ghost/chat", strings.NewReader(chatBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Agent doesn't exist, expect 500 error from IPC
	if w.Code != 500 {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error == nil {
		t.Fatalf("expected error envelope, got nil")
	}
}

func TestAgentChat_BadJSON(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("POST", "/api/v1/tenants/marketing/agents/assistant/chat", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Agent SSE stream
// ---------------------------------------------------------------------------

func TestAgentChatStream_MissingMessage(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("GET", "/api/v1/tenants/marketing/agents/assistant/stream", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error == nil || env.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("expected INVALID_REQUEST, got %v", env.Error)
	}
}

func TestAgentChatStream_Success(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	// First create the agent
	body := `{"name":"streamer","model":"mock","system_prompt":"stream test","max_turns":5}`
	req := httptest.NewRequest("POST", "/api/v1/tenants/marketing/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create agent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Now stream
	req = httptest.NewRequest("GET", "/api/v1/tenants/marketing/agents/streamer/stream?message=Hello", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("stream: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}

	// Verify we got at least a "message" event in the body
	rawBody := w.Body.String()
	if !strings.Contains(rawBody, "event: message") {
		t.Fatalf("SSE body missing 'event: message', got:\n%s", rawBody)
	}
	if !strings.Contains(rawBody, "event: done") {
		t.Fatalf("SSE body missing 'event: done', got:\n%s", rawBody)
	}
}

func TestAgentChatStream_EventFormat(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	// Create agent
	body := `{"name":"evttest","model":"mock","system_prompt":"","max_turns":5}`
	req := httptest.NewRequest("POST", "/api/v1/tenants/finance/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}

	req = httptest.NewRequest("GET", "/api/v1/tenants/finance/agents/evttest/stream?message=hi", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Parse SSE events from body
	scanner := bufio.NewScanner(bytes.NewReader(w.Body.Bytes()))
	var events []map[string]string
	current := map[string]string{}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(current) > 0 {
				events = append(events, current)
				current = map[string]string{}
			}
			continue
		}
		if strings.HasPrefix(line, "event: ") {
			current["event"] = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			current["data"] = strings.TrimPrefix(line, "data: ")
		}
	}
	if len(current) > 0 {
		events = append(events, current)
	}

	if len(events) < 2 {
		t.Fatalf("expected at least 2 SSE events, got %d", len(events))
	}

	// First event should be a message event with valid JSON
	msgEvt := events[0]
	if msgEvt["event"] != "message" {
		t.Fatalf("first event should be 'message', got %q", msgEvt["event"])
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(msgEvt["data"]), &payload); err != nil {
		t.Fatalf("message event data is not valid JSON: %v", err)
	}
	if payload["type"] != "thinking" && payload["type"] != "text" {
		t.Fatalf("expected type=thinking or text, got %v", payload["type"])
	}

	// Last event should be done
	doneEvt := events[len(events)-1]
	if doneEvt["event"] != "done" {
		t.Fatalf("last event should be 'done', got %q", doneEvt["event"])
	}
}

// ---------------------------------------------------------------------------
// Policies
// ---------------------------------------------------------------------------

func TestPolicyTest_Allow(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	body := `{"Tenant":"finance","Action":"tool_call","Tool":"shell","Agent":"bot","User":"alice"}`
	req := httptest.NewRequest("POST", "/api/v1/policies/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error != nil {
		t.Fatalf("expected no error, got %v", env.Error)
	}
	if env.Data == nil {
		t.Fatalf("expected data (policy decision), got nil")
	}
}

func TestPolicyTest_BadJSON(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("POST", "/api/v1/policies/test", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error == nil || env.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("expected INVALID_REQUEST, got %v", env.Error)
	}
}

// ---------------------------------------------------------------------------
// Skills
// ---------------------------------------------------------------------------

func TestSkillList_Empty(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("GET", "/api/v1/skills", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error != nil {
		t.Fatalf("expected no error, got %v", env.Error)
	}
	// Empty registry returns a slice (possibly nil/empty)
	if env.Data == nil {
		t.Fatalf("expected non-nil data payload (empty list is ok), got nil")
	}
}

func TestSkillInstall_BadPath(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	body := `{"path":"/nonexistent/skill/dir"}`
	req := httptest.NewRequest("POST", "/api/v1/skills", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	// Install with a bad path should fail
	if w.Code != 500 {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error == nil || env.Error.Code != "INSTALL_FAILED" {
		t.Fatalf("expected INSTALL_FAILED error, got %v", env.Error)
	}
}

func TestSkillInstall_BadJSON(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("POST", "/api/v1/skills", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error == nil || env.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("expected INVALID_REQUEST, got %v", env.Error)
	}
}

func TestSkillImportOpenClaw_NoLoader(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	// skill runtime has no openclaw loader configured by default
	body := `{"path":"/some/openclaw/skill"}`
	req := httptest.NewRequest("POST", "/api/v1/skills/import/openclaw", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 500 {
		t.Fatalf("expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error == nil || env.Error.Code != "IMPORT_FAILED" {
		t.Fatalf("expected IMPORT_FAILED, got %v", env.Error)
	}
}

func TestSkillImportOpenClaw_BadJSON(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("POST", "/api/v1/skills/import/openclaw", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error == nil || env.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("expected INVALID_REQUEST, got %v", env.Error)
	}
}

// ---------------------------------------------------------------------------
// Audit
// ---------------------------------------------------------------------------

func TestAuditQuery_EmptyDB(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("GET", "/api/v1/audit?tenant=finance", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error != nil {
		t.Fatalf("expected no error, got %v", env.Error)
	}
}

func TestAuditQuery_NoFilter(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("GET", "/api/v1/audit", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuditQuery_WithAllFilters(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("GET", "/api/v1/audit?tenant=finance&action=tool_call&user=alice", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Federation
// ---------------------------------------------------------------------------

func TestFederationPeers_Empty(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("GET", "/api/v1/federation/peers", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error != nil {
		t.Fatalf("expected no error, got %v", env.Error)
	}
}

func TestFederationJoin_Success(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	body := `{"name":"node-b","endpoint":"https://node-b.example.com","status":0}`
	req := httptest.NewRequest("POST", "/api/v1/federation/peers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error != nil {
		t.Fatalf("expected no error, got %v", env.Error)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", env.Data)
	}
	if data["status"] != "joined" {
		t.Fatalf("expected status=joined, got %v", data["status"])
	}
	if data["peer"] != "node-b" {
		t.Fatalf("expected peer=node-b, got %v", data["peer"])
	}
}

func TestFederationJoin_BadJSON(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("POST", "/api/v1/federation/peers", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error == nil || env.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("expected INVALID_REQUEST, got %v", env.Error)
	}
}

func TestFederationPeers_AfterJoin(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	// Join a peer
	body := `{"name":"node-c","endpoint":"https://node-c.example.com","status":0}`
	req := httptest.NewRequest("POST", "/api/v1/federation/peers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("join: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// List peers — should now have one
	req = httptest.NewRequest("GET", "/api/v1/federation/peers", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	peers, ok := env.Data.([]any)
	if !ok {
		t.Fatalf("expected peer slice, got %T", env.Data)
	}
	if len(peers) == 0 {
		t.Fatalf("expected at least 1 peer after join, got 0")
	}
}

// ---------------------------------------------------------------------------
// Auth / OIDC
// ---------------------------------------------------------------------------

func TestOIDCLogin(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("GET", "/api/v1/auth/oidc/login", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error != nil {
		t.Fatalf("expected no error, got %v", env.Error)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", env.Data)
	}
	if data["status"] != "not_configured" {
		t.Fatalf("expected status=not_configured, got %v", data["status"])
	}
}

func TestOIDCCallback_MissingCode(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("GET", "/api/v1/auth/oidc/callback", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error == nil || env.Error.Code != "MISSING_CODE" {
		t.Fatalf("expected MISSING_CODE error, got %v", env.Error)
	}
}

func TestOIDCCallback_WithCode(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("GET", "/api/v1/auth/oidc/callback?code=abc123&state=xyzstate", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	if env.Error != nil {
		t.Fatalf("expected no error, got %v", env.Error)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", env.Data)
	}
	if data["code"] != "abc123" {
		t.Fatalf("expected code=abc123, got %v", data["code"])
	}
	if data["state"] != "xyzstate" {
		t.Fatalf("expected state=xyzstate, got %v", data["state"])
	}
}

func TestOIDCCallback_WithCodeNoState(t *testing.T) {
	_, bus := setupKernel(t)
	srv := NewServer(bus, nil)

	req := httptest.NewRequest("GET", "/api/v1/auth/oidc/callback?code=token42", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", env.Data)
	}
	// state should be empty string when not provided
	if data["state"] != "" {
		t.Fatalf("expected empty state, got %v", data["state"])
	}
}

// ---------------------------------------------------------------------------
// Shared helpers — keep unused import sentinels
// ---------------------------------------------------------------------------

func init() {
	_ = time.Now // suppress unused import
	_ = http.StatusOK

	// Ensure module types are referenced to avoid "imported and not used" errors.
	_ = audit.QueryFilter{}
	_ = skill.NewRuntime
	_ = federation.NewModule
	_ = policy.CheckRequest{}
}
