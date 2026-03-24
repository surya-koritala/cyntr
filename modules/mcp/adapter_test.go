package mcp

import (
	"testing"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

func TestMCPToolAdapterInterface(t *testing.T) {
	var _ agent.Tool = (*MCPToolAdapter)(nil)
}

func TestMCPToolAdapterName(t *testing.T) {
	a := NewMCPToolAdapter(nil, "github", MCPToolDef{Name: "create_issue"})
	if a.Name() != "mcp_github_create_issue" {
		t.Fatalf("expected mcp_github_create_issue, got %q", a.Name())
	}
}

func TestConvertInputSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":    map[string]any{"type": "string", "description": "File path"},
			"content": map[string]any{"type": "string", "description": "Content"},
		},
		"required": []any{"path"},
	}
	params := convertInputSchema(schema)
	if len(params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(params))
	}
	if !params["path"].Required {
		t.Fatal("path should be required")
	}
	if params["content"].Required {
		t.Fatal("content should not be required")
	}
}

func TestConvertEmptySchema(t *testing.T) {
	params := convertInputSchema(map[string]any{})
	if len(params) != 0 {
		t.Fatalf("expected 0 params, got %d", len(params))
	}
}
