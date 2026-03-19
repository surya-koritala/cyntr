package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type ShellTool struct{}

func (t *ShellTool) Name() string        { return "shell_exec" }
func (t *ShellTool) Description() string { return "Execute a shell command and return its output" }
func (t *ShellTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"command": {Type: "string", Description: "The shell command to execute", Required: true},
	}
}

func (t *ShellTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	command := input["command"]
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	// Truncate to 64KB
	if len(output) > 65536 {
		output = output[:65536] + "\n... (truncated)"
	}

	if err != nil {
		return output, fmt.Errorf("command failed: %w", err)
	}
	return output, nil
}
