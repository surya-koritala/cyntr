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

	// Mock agent runtime
	bus.Handle("agent_runtime", "agent.chat", func(msg ipc.Message) (ipc.Message, error) {
		req := msg.Payload.(agent.ChatRequest)
		return ipc.Message{
			Type: ipc.MessageTypeResponse,
			Payload: agent.ChatResponse{
				Agent:   req.Agent,
				Content: "Delegated response for: " + req.Message,
			},
		}, nil
	})

	tool := NewDelegateTool(bus)
	result, err := tool.Execute(context.Background(), map[string]string{
		"tenant": "demo", "agent": "specialist", "message": "Analyze this data",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsStr(result, "Delegated response") {
		t.Fatalf("got %q", result)
	}
	if !containsStr(result, "Analyze this data") {
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
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, err := tool.Execute(ctx, map[string]string{
		"tenant": "demo", "agent": "nonexistent", "message": "test",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
