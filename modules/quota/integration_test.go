package quota_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/agent/providers"
	"github.com/cyntr-dev/cyntr/modules/quota"
)

// TestAgentRuntimeUnlimitedWhenQuotaModuleAbsent verifies the runtime keeps
// working when no quota module has been registered on the bus.
func TestAgentRuntimeUnlimitedWhenQuotaModuleAbsent(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	rt := agent.NewRuntime()
	rt.RegisterProvider(providers.NewMock("hi"))
	rt.SetToolRegistry(agent.NewToolRegistry())

	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{Name: "a", Tenant: "t1", Model: "mock", MaxTurns: 2},
	})

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{Agent: "a", Tenant: "t1", User: "u", Message: "hello"},
	})
	if err != nil {
		t.Fatalf("chat should succeed without quota module: %v", err)
	}
	if resp.Payload.(agent.ChatResponse).Content != "hi" {
		t.Fatalf("expected mock response, got %+v", resp.Payload)
	}
}

// TestAgentRuntimeQuotaRateDeny verifies the runtime returns a "quota exceeded"
// ChatResponse (no panic, no transport error) when the quota module denies the
// rate check.
func TestAgentRuntimeQuotaRateDeny(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	// Register the quota module first with a tight rate cap.
	qm := quota.New(filepath.Join(t.TempDir(), "quota.db"))
	if err := qm.Init(context.Background(), &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("quota init: %v", err)
	}
	if err := qm.Start(context.Background()); err != nil {
		t.Fatalf("quota start: %v", err)
	}
	defer qm.Stop(context.Background())

	qm.Enforcer().SetConfig(quota.QuotaConfig{Tenant: "t1", RequestsPerMinute: 1})

	rt := agent.NewRuntime()
	rt.RegisterProvider(providers.NewMock("hi"))
	rt.SetToolRegistry(agent.NewToolRegistry())
	rt.Init(context.Background(), &kernel.Services{Bus: bus})
	rt.Start(context.Background())
	defer rt.Stop(context.Background())

	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{Name: "a", Tenant: "t1", Model: "mock", MaxTurns: 2},
	})

	// First call: under quota.
	resp1, err := bus.Request(reqCtx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{Agent: "a", Tenant: "t1", User: "u", Message: "1"},
	})
	if err != nil {
		t.Fatalf("first chat: %v", err)
	}
	if resp1.Payload.(agent.ChatResponse).Content != "hi" {
		t.Fatalf("first chat should succeed, got %+v", resp1.Payload)
	}

	// Second call: rate-limited → expect quota exceeded message, not a transport error.
	resp2, err := bus.Request(reqCtx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{Agent: "a", Tenant: "t1", User: "u", Message: "2"},
	})
	if err != nil {
		t.Fatalf("second chat should not error at transport: %v", err)
	}
	cr2 := resp2.Payload.(agent.ChatResponse)
	if cr2.Content == "hi" {
		t.Fatalf("second chat should have been rate-limited, got %+v", cr2)
	}
}
