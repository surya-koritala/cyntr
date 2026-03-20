package agent_test

import (
	"context"
	"testing"

	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/agent/providers"
)

func TestMockProviderChat(t *testing.T) {
	p := providers.NewMock("Hello from mock!")

	resp, err := p.Chat(context.Background(), []agent.Message{
		{Role: agent.RoleUser, Content: "Hi"},
	}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content != "Hello from mock!" {
		t.Fatalf("expected mock response, got %q", resp.Content)
	}
	if resp.Role != agent.RoleAssistant {
		t.Fatalf("expected assistant role, got %s", resp.Role)
	}
}

func TestMockProviderWithToolCall(t *testing.T) {
	p := providers.NewMockWithToolCall("shell_exec", map[string]string{"command": "ls"})

	resp, err := p.Chat(context.Background(), []agent.Message{
		{Role: agent.RoleUser, Content: "List files"},
	}, []agent.ToolDef{{Name: "shell_exec", Description: "Run shell command"}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "shell_exec" {
		t.Fatalf("expected shell_exec, got %q", resp.ToolCalls[0].Name)
	}
}

func TestMockProviderName(t *testing.T) {
	p := providers.NewMock("test")
	if p.Name() != "mock" {
		t.Fatalf("expected 'mock', got %q", p.Name())
	}
}
