package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type FileReadTool struct{}

func (t *FileReadTool) Name() string        { return "file_read" }
func (t *FileReadTool) Description() string { return "Read the contents of a file" }
func (t *FileReadTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"path": {Type: "string", Description: "File path to read", Required: true},
	}
}
func (t *FileReadTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	path := input["path"]
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("file not found: %w", err)
	}
	if info.Size() > 1<<20 {
		return "", fmt.Errorf("file too large (%d bytes, max 1MB)", info.Size())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(data), nil
}

type FileWriteTool struct{}

func (t *FileWriteTool) Name() string        { return "file_write" }
func (t *FileWriteTool) Description() string { return "Write content to a file" }
func (t *FileWriteTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"path":    {Type: "string", Description: "File path to write", Required: true},
		"content": {Type: "string", Description: "Content to write", Required: true},
	}
}
func (t *FileWriteTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	path := input["path"]
	content := input["content"]
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return fmt.Sprintf("written %d bytes to %s", len(content), path), nil
}

type FileSearchTool struct{}

func (t *FileSearchTool) Name() string        { return "file_search" }
func (t *FileSearchTool) Description() string { return "Search for files matching a glob pattern" }
func (t *FileSearchTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"directory": {Type: "string", Description: "Directory to search in", Required: true},
		"pattern":   {Type: "string", Description: "Glob pattern (e.g. *.go)", Required: true},
	}
}
func (t *FileSearchTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	dir := input["directory"]
	pattern := input["pattern"]
	if dir == "" || pattern == "" {
		return "", fmt.Errorf("directory and pattern required")
	}
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return "", fmt.Errorf("glob: %w", err)
	}
	if len(matches) > 100 {
		matches = matches[:100]
	}
	if len(matches) == 0 {
		return "no matches found", nil
	}
	result := ""
	for _, m := range matches {
		result += m + "\n"
	}
	return result, nil
}
