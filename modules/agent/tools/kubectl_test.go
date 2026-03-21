package tools

import (
	"context"
	"strings"
	"testing"
)

func TestKubectlToolName(t *testing.T) {
	if NewKubectlTool().Name() != "kubectl" {
		t.Fatal("wrong name")
	}
}

func TestKubectlToolRejectsWrite(t *testing.T) {
	tool := NewKubectlTool()
	for _, cmd := range []string{"apply", "delete", "create", "patch", "replace", "scale"} {
		_, err := tool.Execute(context.Background(), map[string]string{"command": cmd})
		if err == nil {
			t.Fatalf("expected error for write command %q", cmd)
		}
	}
}

func TestKubectlToolAllowsRead(t *testing.T) {
	tool := NewKubectlTool()
	// These should not error on command validation (may fail on kubectl not configured)
	for _, cmd := range []string{"get", "describe", "logs", "top", "version"} {
		_, err := tool.Execute(context.Background(), map[string]string{"command": cmd})
		// Will error if kubectl not installed, but NOT with "not allowed"
		if err != nil && strings.Contains(err.Error(), "not allowed") {
			t.Fatalf("read command %q should be allowed", cmd)
		}
	}
}

func TestKubectlToolMissingCommand(t *testing.T) {
	tool := NewKubectlTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestKubectlToolRejectsMutatingFlags(t *testing.T) {
	tool := NewKubectlTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"command": "get", "resource": "pods", "flags": "--delete-collection",
	})
	if err == nil {
		t.Fatal("expected error for mutating flag")
	}
}

func TestKubectlToolParams(t *testing.T) {
	tool := NewKubectlTool()
	params := tool.Parameters()
	if !params["command"].Required {
		t.Fatal("command should be required")
	}
	if params["namespace"].Required {
		t.Fatal("namespace should not be required")
	}
}
