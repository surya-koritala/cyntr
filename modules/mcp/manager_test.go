package mcp

import "testing"

func TestManagerName(t *testing.T) {
	m := NewManager(nil)
	if m.Name() != "mcp" {
		t.Fatal("wrong name")
	}
}

func TestManagerDependencies(t *testing.T) {
	m := NewManager(nil)
	deps := m.Dependencies()
	if len(deps) != 1 || deps[0] != "agent_runtime" {
		t.Fatalf("expected [agent_runtime], got %v", deps)
	}
}
