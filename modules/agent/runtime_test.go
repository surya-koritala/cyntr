package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/agent/providers"
)

func TestRuntimeImplementsModule(t *testing.T) {
	var _ kernel.Module = (*agent.Runtime)(nil)
}

func TestRuntimeCreateAgentAndChat(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	mockProvider := providers.NewMock("Hello from the agent!")
	toolReg := agent.NewToolRegistry()

	rt := agent.NewRuntime()
	rt.RegisterProvider(mockProvider)
	rt.SetToolRegistry(toolReg)

	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	// Create an agent
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: "test-agent", Tenant: "finance", Model: "mock",
			SystemPrompt: "You are helpful.", Tools: []string{}, MaxTurns: 10,
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("expected 'ok', got %v", resp.Payload)
	}

	// Chat with the agent
	resp, err = bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{Agent: "test-agent", Tenant: "finance", User: "jane@corp.com", Message: "Hi"},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	chatResp, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		t.Fatalf("expected ChatResponse, got %T", resp.Payload)
	}
	if chatResp.Content != "Hello from the agent!" {
		t.Fatalf("expected mock response, got %q", chatResp.Content)
	}
}

func TestRuntimeChatWithToolUse(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	mockProvider := providers.NewMockWithToolCall("echo", map[string]string{"text": "test"})
	toolReg := agent.NewToolRegistry()
	toolReg.Register(&echoToolImpl{})

	rt := agent.NewRuntime()
	rt.RegisterProvider(mockProvider)
	rt.SetToolRegistry(toolReg)

	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Create agent with echo tool
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: "tool-agent", Tenant: "finance", Model: "mock",
			Tools: []string{"echo"}, MaxTurns: 10,
		},
	})

	// Chat — should trigger tool call then final response
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{Agent: "tool-agent", Tenant: "finance", User: "jane@corp.com", Message: "Echo something"},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	chatResp := resp.Payload.(agent.ChatResponse)
	if chatResp.Content != "Tool result processed." {
		t.Fatalf("expected final response, got %q", chatResp.Content)
	}
	if len(chatResp.ToolsUsed) != 1 || chatResp.ToolsUsed[0] != "echo" {
		t.Fatalf("expected [echo], got %v", chatResp.ToolsUsed)
	}
}

func TestRuntimeChatAgentNotFound(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	rt := agent.NewRuntime()
	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{Agent: "nonexistent", Tenant: "finance", Message: "Hi"},
	})
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestRuntimeListAgents(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	rt := agent.NewRuntime()
	rt.RegisterProvider(providers.NewMock("test"))

	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Create two agents
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{Name: "agent-a", Tenant: "finance", Model: "mock"},
	})
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{Name: "agent-b", Tenant: "finance", Model: "mock"},
	})

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.list",
		Payload: "finance",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	names, ok := resp.Payload.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", resp.Payload)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(names))
	}
}

func TestRuntimeHealthy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	rt := agent.NewRuntime()
	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	health := rt.Health(ctx)
	if !health.Healthy {
		t.Fatalf("expected healthy: %s", health.Message)
	}
}

// echoToolImpl implements agent.Tool for testing.
type echoToolImpl struct{}

func (e *echoToolImpl) Name() string        { return "echo" }
func (e *echoToolImpl) Description() string { return "Echoes input" }
func (e *echoToolImpl) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{"text": {Type: "string", Description: "Text to echo", Required: true}}
}
func (e *echoToolImpl) Execute(ctx context.Context, input map[string]string) (string, error) {
	return "echo: " + input["text"], nil
}
