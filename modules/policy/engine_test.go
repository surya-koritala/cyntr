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

	engine := NewEngine(policyPath, "")
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
	engine := NewEngine(policyPath, "")
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
	engine := NewEngine("/nonexistent/policy.yaml", "")
	bus := ipc.NewBus()
	defer bus.Close()
	err := engine.Init(context.Background(), &kernel.Services{Bus: bus})
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestEngineCompositionYAMLAllowRegoDeny verifies fail-closed composition:
// YAML allows the request, but the Rego policy denies it, so the engine
// returns Deny.
func TestEngineCompositionYAMLAllowRegoDeny(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(yamlPath, []byte(`
rules:
  - name: allow-all-tools
    tenant: "*"
    action: tool_call
    tool: "*"
    agent: "*"
    decision: allow
    priority: 10
`), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	regoPath := filepath.Join(dir, "policy.rego")
	if err := os.WriteFile(regoPath, []byte(`package cyntr.policy

default decision := "allow"

decision := "deny" if {
  input.tool == "shell_exec"
}
`), 0644); err != nil {
		t.Fatalf("write rego: %v", err)
	}

	bus := ipc.NewBus()
	defer bus.Close()
	engine := NewEngine(yamlPath, regoPath)
	ctx := context.Background()
	if err := engine.Init(ctx, &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer engine.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// YAML would allow shell_exec, but Rego denies it.
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "policy", Topic: "policy.check",
		Payload: CheckRequest{Tenant: "marketing", Action: "tool_call", Tool: "shell_exec", Agent: "assistant"},
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	checkResp, ok := resp.Payload.(CheckResponse)
	if !ok {
		t.Fatalf("expected CheckResponse, got %T", resp.Payload)
	}
	if checkResp.Decision != Deny {
		t.Fatalf("expected deny (rego overrides yaml allow), got %s — reason=%q", checkResp.Decision, checkResp.Reason)
	}
	if checkResp.Rule != "rego" {
		t.Fatalf("expected rule=rego, got %q", checkResp.Rule)
	}

	// And a request Rego allows should pass through with YAML's allow.
	resp, err = bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "policy", Topic: "policy.check",
		Payload: CheckRequest{Tenant: "marketing", Action: "tool_call", Tool: "http_request", Agent: "assistant"},
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	checkResp = resp.Payload.(CheckResponse)
	if checkResp.Decision != Allow {
		t.Fatalf("expected allow, got %s", checkResp.Decision)
	}
}

// TestEngineCompositionYAMLDenyShortCircuits verifies that when YAML denies,
// Rego is not consulted (and an even-if-broken Rego allow doesn't override).
func TestEngineCompositionYAMLDenyShortCircuits(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(yamlPath, []byte(`
rules:
  - name: deny-shell
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 20
`), 0644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	regoPath := filepath.Join(dir, "policy.rego")
	if err := os.WriteFile(regoPath, []byte(`package cyntr.policy

default decision := "allow"
`), 0644); err != nil {
		t.Fatalf("write rego: %v", err)
	}

	bus := ipc.NewBus()
	defer bus.Close()
	engine := NewEngine(yamlPath, regoPath)
	ctx := context.Background()
	if err := engine.Init(ctx, &kernel.Services{Bus: bus}); err != nil {
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
		Payload: CheckRequest{Tenant: "marketing", Action: "tool_call", Tool: "shell_exec", Agent: "assistant"},
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	checkResp := resp.Payload.(CheckResponse)
	if checkResp.Decision != Deny {
		t.Fatalf("expected deny from yaml, got %s", checkResp.Decision)
	}
	if checkResp.Rule != "deny-shell" {
		t.Fatalf("expected rule=deny-shell (yaml short-circuit), got %q", checkResp.Rule)
	}
}
