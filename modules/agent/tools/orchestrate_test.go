package tools

import "testing"

func TestOrchestrateToolName(t *testing.T) {
	tool := NewOrchestrateTool(nil)
	if tool.Name() != "orchestrate_agents" {
		t.Fatal("wrong name")
	}
}

func TestOrchestrateToolParams(t *testing.T) {
	tool := NewOrchestrateTool(nil)
	params := tool.Parameters()
	if _, ok := params["tasks"]; !ok {
		t.Fatal("missing tasks param")
	}
}
