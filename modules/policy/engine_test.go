package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func TestEngineImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Engine)(nil)
}

func TestEngineHandlesPolicyCheck(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte(`
rules:
  - name: allow-http
    tenant: "*"
    action: tool_call
    tool: http_request
    agent: "*"
    decision: allow
    priority: 10
  - name: deny-shell
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 20
`), 0644)

	bus := ipc.NewBus()
	defer bus.Close()

	engine := NewEngine(policyPath)
	svc := &kernel.Services{Bus: bus}

	ctx := context.Background()
	if err := engine.Init(ctx, svc); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer engine.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "policy", Topic: "policy.check",
		Payload: CheckRequest{Tenant: "marketing", Action: "tool_call", Tool: "http_request", Agent: "assistant"},
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	checkResp, ok := resp.Payload.(CheckResponse)
	if !ok {
		t.Fatalf("expected CheckResponse, got %T", resp.Payload)
	}
	if checkResp.Decision != Allow {
		t.Fatalf("expected allow, got %s", checkResp.Decision)
	}

	resp, err = bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "policy", Topic: "policy.check",
		Payload: CheckRequest{Tenant: "finance", Action: "tool_call", Tool: "shell_exec"},
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	checkResp = resp.Payload.(CheckResponse)
	if checkResp.Decision != Deny {
		t.Fatalf("expected deny, got %s", checkResp.Decision)
	}
}

func TestEngineHealthy(t *testing.T) {
	dir := t.TempDir()
	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte("rules: []\n"), 0644)

	bus := ipc.NewBus()
	defer bus.Close()
	engine := NewEngine(policyPath)
	ctx := context.Background()
	engine.Init(ctx, &kernel.Services{Bus: bus})
	engine.Start(ctx)
	defer engine.Stop(ctx)

	health := engine.Health(ctx)
	if !health.Healthy {
		t.Fatal("expected healthy")
	}
}

func TestEngineInitFailsBadPolicy(t *testing.T) {
	engine := NewEngine("/nonexistent/policy.yaml")
	bus := ipc.NewBus()
	defer bus.Close()
	err := engine.Init(context.Background(), &kernel.Services{Bus: bus})
	if err == nil {
		t.Fatal("expected error")
	}
}
