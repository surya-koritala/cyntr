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
	_ = http.StatusOK
}
