package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/agent/providers"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

func TestAgentRuntimeWithPolicyAndTools(t *testing.T) {
	dir := t.TempDir()

	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte(`
rules:
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

	// Set up modules
	policyEngine := policy.NewEngine(policyPath)
	agentRuntime := agent.NewRuntime()
	agentRuntime.RegisterProvider(providers.NewMock("I processed your request successfully."))

	if err := k.Register(policyEngine); err != nil {
		t.Fatalf("register policy: %v", err)
	}
	if err := k.Register(agentRuntime); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer k.Stop(ctx)

	bus := k.Bus()
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Create an agent
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: "assistant", Tenant: "marketing", Model: "mock",
			SystemPrompt: "You are a marketing assistant.",
			MaxTurns:     5,
		},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("expected ok, got %v", resp.Payload)
	}

	// Check policy first (like proxy would)
	policyResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "policy", Topic: "policy.check",
		Payload: policy.CheckRequest{
			Tenant: "marketing", Action: "model_call", Tool: "claude",
			Agent: "assistant", User: "bob@corp.com",
		},
	})
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	checkResp := policyResp.Payload.(policy.CheckResponse)
	if checkResp.Decision != policy.Allow {
		t.Fatalf("expected allow, got %s", checkResp.Decision)
	}

	// Chat with the agent
	chatResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent: "assistant", Tenant: "marketing",
			User: "bob@corp.com", Message: "Help me write a press release",
		},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	result := chatResp.Payload.(agent.ChatResponse)
	if result.Content != "I processed your request successfully." {
		t.Fatalf("unexpected response: %q", result.Content)
	}
	if result.Agent != "assistant" {
		t.Fatalf("expected agent 'assistant', got %q", result.Agent)
	}

	// List agents
	listResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.list",
		Payload: "marketing",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	agents := listResp.Payload.([]string)
	if len(agents) != 1 || agents[0] != "assistant" {
		t.Fatalf("expected [assistant], got %v", agents)
	}
}
