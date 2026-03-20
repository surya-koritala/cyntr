package agent

import (
	"context"
	"testing"
)

type echoTool struct{}

func (t *echoTool) Name() string        { return "echo" }
func (t *echoTool) Description() string { return "Echoes input" }
func (t *echoTool) Parameters() map[string]ToolParam {
	return map[string]ToolParam{"text": {Type: "string", Description: "Text to echo", Required: true}}
}
func (t *echoTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	return "echo: " + input["text"], nil
}

func TestToolRegistryRegisterAndGet(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})

	tool, ok := reg.Get("echo")
	if !ok {
		t.Fatal("expected to find echo tool")
	}
	if tool.Name() != "echo" {
		t.Fatalf("expected echo, got %q", tool.Name())
	}
}

func TestToolRegistryGetNotFound(t *testing.T) {
	reg := NewToolRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestToolRegistryList(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})

	tools := reg.List()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0] != "echo" {
		t.Fatalf("expected echo, got %q", tools[0])
	}
}

func TestToolRegistryToolDefs(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})

	defs := reg.ToolDefs([]string{"echo"})
	if len(defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(defs))
	}
	if defs[0].Name != "echo" {
		t.Fatalf("expected echo, got %q", defs[0].Name)
	}
}

func TestToolRegistryExecute(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})

	result, err := reg.Execute(context.Background(), "echo", map[string]string{"text": "hello"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result != "echo: hello" {
		t.Fatalf("expected 'echo: hello', got %q", result)
	}
}

func TestToolRegistryExecuteNotFound(t *testing.T) {
	reg := NewToolRegistry()
	_, err := reg.Execute(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
