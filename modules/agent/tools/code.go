package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type CodeInterpreterTool struct {
	timeout time.Duration
}

func NewCodeInterpreterTool() *CodeInterpreterTool {
	return &CodeInterpreterTool{timeout: 30 * time.Second}
}

func (t *CodeInterpreterTool) Name() string { return "code_interpreter" }
func (t *CodeInterpreterTool) Description() string {
	return "Execute Python or JavaScript code and return the output. Use for calculations, data processing, and analysis."
}
func (t *CodeInterpreterTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"language": {Type: "string", Description: "Language: python or javascript", Required: true},
		"code":     {Type: "string", Description: "Code to execute", Required: true},
	}
}

func (t *CodeInterpreterTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	lang := input["language"]
	code := input["code"]
	if lang == "" || code == "" {
		return "", fmt.Errorf("language and code are required")
	}

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	// Write code to temp file
	dir, err := os.MkdirTemp("", "cyntr-code-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	var cmd *exec.Cmd
	switch lang {
	case "python", "python3":
		filePath := filepath.Join(dir, "script.py")
		os.WriteFile(filePath, []byte(code), 0644)
		cmd = exec.CommandContext(ctx, "python3", filePath)
	case "javascript", "js", "node":
		filePath := filepath.Join(dir, "script.js")
		os.WriteFile(filePath, []byte(code), 0644)
		cmd = exec.CommandContext(ctx, "node", filePath)
	default:
		return "", fmt.Errorf("unsupported language: %s (supported: python, javascript)", lang)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Dir = dir

	err = cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += "stderr: " + stderr.String()
	}

	if len(output) > 65536 {
		output = output[:65536] + "\n...(truncated)"
	}

	if err != nil {
		return output, fmt.Errorf("execution error: %w", err)
	}
	return output, nil
}
