package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileReadTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	tool := &FileReadTool{}
	result, err := tool.Execute(context.Background(), map[string]string{"path": path})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if result != "hello world" {
		t.Fatalf("got %q", result)
	}
}

func TestFileReadToolNotFound(t *testing.T) {
	tool := &FileReadTool{}
	_, err := tool.Execute(context.Background(), map[string]string{"path": "/nonexistent/file"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFileReadToolEmpty(t *testing.T) {
	tool := &FileReadTool{}
	_, err := tool.Execute(context.Background(), map[string]string{"path": ""})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFileWriteTool(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "output.txt")

	tool := &FileWriteTool{}
	result, err := tool.Execute(context.Background(), map[string]string{"path": path, "content": "test content"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !strings.Contains(result, "12 bytes") {
		t.Fatalf("got %q", result)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "test content" {
		t.Fatalf("file content: %q", string(data))
	}
}

func TestFileWriteToolEmptyPath(t *testing.T) {
	tool := &FileWriteTool{}
	_, err := tool.Execute(context.Background(), map[string]string{"path": "", "content": "x"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFileSearchTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b"), 0644)
	os.WriteFile(filepath.Join(dir, "c.go"), []byte("c"), 0644)

	tool := &FileSearchTool{}
	result, err := tool.Execute(context.Background(), map[string]string{"directory": dir, "pattern": "*.txt"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(result, "a.txt") {
		t.Fatalf("missing a.txt: %q", result)
	}
	if !strings.Contains(result, "b.txt") {
		t.Fatalf("missing b.txt: %q", result)
	}
	if strings.Contains(result, "c.go") {
		t.Fatal("should not match .go")
	}
}

func TestFileSearchToolNoMatches(t *testing.T) {
	dir := t.TempDir()
	tool := &FileSearchTool{}
	result, _ := tool.Execute(context.Background(), map[string]string{"directory": dir, "pattern": "*.xyz"})
	if result != "no matches found" {
		t.Fatalf("got %q", result)
	}
}

func TestFileSearchToolMissingParams(t *testing.T) {
	tool := &FileSearchTool{}
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFileReadToolName(t *testing.T) {
	if (&FileReadTool{}).Name() != "file_read" {
		t.Fatal("wrong name")
	}
}
func TestFileWriteToolName(t *testing.T) {
	if (&FileWriteTool{}).Name() != "file_write" {
		t.Fatal("wrong name")
	}
}
func TestFileSearchToolName(t *testing.T) {
	if (&FileSearchTool{}).Name() != "file_search" {
		t.Fatal("wrong name")
	}
}
