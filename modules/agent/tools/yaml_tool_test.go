package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYAMLToolBasic(t *testing.T) {
	def := YAMLToolDef{
		Name:        "test_echo",
		Description: "Echo test",
		Command:     "echo hello",
		Timeout:     "5s",
	}
	tool, err := NewYAMLTool(def)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tool.Name() != "test_echo" {
		t.Fatal("wrong name")
	}

	result, err := tool.Execute(context.Background(), map[string]string{})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Fatalf("expected hello, got: %s", result)
	}
}

func TestYAMLToolWithParams(t *testing.T) {
	def := YAMLToolDef{
		Name:        "greet",
		Description: "Greet someone",
		Command:     "echo Hello {{.name}}",
		Parameters: map[string]YAMLToolParamDef{
			"name": {Type: "string", Description: "Name to greet", Required: true},
		},
	}
	tool, _ := NewYAMLTool(def)

	result, err := tool.Execute(context.Background(), map[string]string{"name": "World"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(result, "Hello World") {
		t.Fatalf("expected 'Hello World', got: %s", result)
	}
}

func TestYAMLToolMissingName(t *testing.T) {
	_, err := NewYAMLTool(YAMLToolDef{Command: "echo"})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestYAMLToolMissingCommand(t *testing.T) {
	_, err := NewYAMLTool(YAMLToolDef{Name: "test"})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestLoadToolsFromDir(t *testing.T) {
	dir := t.TempDir()

	yaml1 := `name: tool_one
description: First tool
command: echo one
timeout: 5s`
	os.WriteFile(filepath.Join(dir, "tool1.yaml"), []byte(yaml1), 0644)

	yaml2 := `name: tool_two
description: Second tool
command: echo two`
	os.WriteFile(filepath.Join(dir, "tool2.yml"), []byte(yaml2), 0644)

	// Non-yaml file should be ignored
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("ignore me"), 0644)

	tools, err := LoadToolsFromDir(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
}

func TestLoadToolsFromNonexistentDir(t *testing.T) {
	tools, err := LoadToolsFromDir("/nonexistent/path")
	if err != nil {
		t.Fatalf("expected nil error for nonexistent dir, got: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(tools))
	}
}
