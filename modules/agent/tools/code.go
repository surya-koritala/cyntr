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
	rpc     *RPCConfig // when set, scripts can call cyntr.call_tool (E21)
}

func NewCodeInterpreterTool() *CodeInterpreterTool {
	return &CodeInterpreterTool{timeout: 30 * time.Second}
}

// EnableRPC turns on the in-script tool bridge so a script can call
// cyntr.call_tool(name, args) in the same turn. Every such call is re-checked
// against policy and audited, so scripting can never reach a tool the agent
// itself couldn't call.
func (t *CodeInterpreterTool) EnableRPC(cfg *RPCConfig) { t.rpc = cfg }

func (t *CodeInterpreterTool) Name() string { return "code_interpreter" }
func (t *CodeInterpreterTool) Description() string {
	return "Execute Python or JavaScript code and return the output. Use for calculations, data processing, and analysis. Scripts may call cyntr.call_tool(name, args) to invoke your other tools and collapse a multi-step pipeline into one turn."
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

	// Tool-RPC bridge (E21): when enabled and we have a caller context,
	// prepend the cyntr.call_tool shim and start a loopback server the script
	// talks to. Every call back is policy-checked + audited for this caller.
	var rpcEnv []string
	if t.rpc != nil {
		if tenant, agentName, user := agent.ToolCaller(ctx); tenant != "" {
			if shim := rpcShimFor(lang); shim != "" {
				bridge := newBridge(tenant, agentName, user, t.rpc)
				if url, stop, err := bridge.serve(); err == nil {
					defer stop()
					rpcEnv = []string{"CYNTR_RPC_URL=" + url, "CYNTR_RPC_TOKEN=" + bridge.token}
					code = shim + "\n" + code
				}
			}
		}
	}

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
	if len(rpcEnv) > 0 {
		cmd.Env = append(os.Environ(), rpcEnv...)
	}

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
