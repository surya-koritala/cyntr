package tools

import (
	"context"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func TestDelegateToolName(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	if NewDelegateTool(bus).Name() != "delegate_agent" {
		t.Fatal()
	}
}

func TestDelegateToolExecute(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	// Mock agent runtime — capture the request so we can assert the tenant came
	// from the tool context, not the model input.
	var gotTenant, gotUser string
	bus.Handle("agent_runtime", "agent.chat", func(msg ipc.Message) (ipc.Message, error) {
		req := msg.Payload.(agent.ChatRequest)
		gotTenant, gotUser = req.Tenant, req.User
		return ipc.Message{
			Type: ipc.MessageTypeResponse,
			Payload: agent.ChatResponse{
				Agent:   req.Agent,
				Content: "Delegated response for: " + req.Message,
			},
		}, nil
	})

	tool := NewDelegateTool(bus)
	// Tenant/user come from the caller context; a model-supplied "tenant" must
	// be ignored.
	ctx := agent.WithToolCaller(context.Background(), "demo", "parent", "jane")
	result, err := tool.Execute(ctx, map[string]string{
		"tenant": "victim", "agent": "specialist", "message": "Analyze this data",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotTenant != "demo" {
		t.Fatalf("tenant must come from caller context (demo), got %q", gotTenant)
	}
	if gotUser != "jane" {
		t.Fatalf("user should be inherited (jane), got %q", gotUser)
	}
	if !containsStr(result, "Delegated response") || !containsStr(result, "Analyze this data") {
		t.Fatalf("got %q", result)
	}
}

func TestDelegateToolMissingParams(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	tool := NewDelegateTool(bus)
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDelegateToolAgentNotFound(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	// No agent_runtime handler registered — should fail
	tool := NewDelegateTool(bus)
	ctx, cancel := context.WithTimeout(agent.WithToolCaller(context.Background(), "demo", "parent", "jane"), 1*time.Second)
	defer cancel()
	_, err := tool.Execute(ctx, map[string]string{
		"agent": "nonexistent", "message": "test",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
