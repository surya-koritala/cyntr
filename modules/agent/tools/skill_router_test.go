package tools

import "testing"

func TestSkillRouterToolName(t *testing.T) {
	tool := NewSkillRouterTool(nil)
	if tool.Name() != "skill_router" {
		t.Fatalf("expected skill_router, got %q", tool.Name())
	}
}

func TestSkillRouterToolParams(t *testing.T) {
	tool := NewSkillRouterTool(nil)
	params := tool.Parameters()
	if _, ok := params["action"]; !ok {
		t.Fatal("missing action param")
	}
	if !params["action"].Required {
		t.Fatal("action should be required")
	}
}

func TestSkillRouterToolDescription(t *testing.T) {
	tool := NewSkillRouterTool(nil)
	if tool.Description() == "" {
		t.Fatal("description should not be empty")
	}
}
