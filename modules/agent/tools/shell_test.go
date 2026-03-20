package tools

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestShellToolName(t *testing.T) {
	tool := &ShellTool{}
	if tool.Name() != "shell_exec" {
		t.Fatalf("expected shell_exec, got %q", tool.Name())
	}
}

func TestShellToolEcho(t *testing.T) {
	tool := &ShellTool{}
	result, err := tool.Execute(context.Background(), map[string]string{"command": "echo hello"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(result) != "hello" {
		t.Fatalf("expected 'hello', got %q", result)
	}
}

func TestShellToolStderr(t *testing.T) {
	tool := &ShellTool{}
	result, _ := tool.Execute(context.Background(), map[string]string{"command": "echo err >&2"})
	if !strings.Contains(result, "err") {
		t.Fatalf("expected stderr in output, got %q", result)
	}
}

func TestShellToolEmptyCommand(t *testing.T) {
	tool := &ShellTool{}
	_, err := tool.Execute(context.Background(), map[string]string{"command": ""})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

func TestShellToolTimeout(t *testing.T) {
	tool := &ShellTool{}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := tool.Execute(ctx, map[string]string{"command": "sleep 60"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestShellToolExitCode(t *testing.T) {
	tool := &ShellTool{}
	_, err := tool.Execute(context.Background(), map[string]string{"command": "exit 1"})
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
}
