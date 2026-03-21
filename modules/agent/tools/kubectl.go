package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type KubectlTool struct{}

func NewKubectlTool() *KubectlTool { return &KubectlTool{} }

func (t *KubectlTool) Name() string { return "kubectl" }
func (t *KubectlTool) Description() string {
	return "Execute read-only kubectl commands against a Kubernetes cluster. Only get/describe/logs/top commands are allowed. Cluster must be configured via kubeconfig."
}
func (t *KubectlTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"command":   {Type: "string", Description: "kubectl subcommand: get, describe, logs, top, version, cluster-info", Required: true},
		"resource":  {Type: "string", Description: "Resource type: pods, services, deployments, nodes, namespaces, ingress, configmaps, events", Required: false},
		"namespace": {Type: "string", Description: "Kubernetes namespace (default: default)", Required: false},
		"name":      {Type: "string", Description: "Specific resource name", Required: false},
		"flags":     {Type: "string", Description: "Additional flags like --all-namespaces, -o json, --tail=100", Required: false},
	}
}

// readOnlyCommands is the allowlist of safe kubectl subcommands.
var readOnlyCommands = map[string]bool{
	"get": true, "describe": true, "logs": true, "top": true,
	"version": true, "cluster-info": true, "api-resources": true,
	"explain": true, "auth": true,
}

func (t *KubectlTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	command := input["command"]
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	// Enforce read-only
	if !readOnlyCommands[command] {
		return "", fmt.Errorf("command %q not allowed — only read-only commands: get, describe, logs, top, version, cluster-info, api-resources, explain", command)
	}

	// Build kubectl command
	args := []string{command}

	if resource := input["resource"]; resource != "" {
		args = append(args, resource)
	}
	if name := input["name"]; name != "" {
		args = append(args, name)
	}

	ns := input["namespace"]
	if ns == "" {
		ns = "default"
	}
	if command != "version" && command != "cluster-info" && command != "api-resources" {
		args = append(args, "-n", ns)
	}

	if flags := input["flags"]; flags != "" {
		// Parse flags safely — reject anything that could mutate
		for _, flag := range strings.Fields(flags) {
			lower := strings.ToLower(flag)
			if strings.Contains(lower, "delete") || strings.Contains(lower, "edit") || strings.Contains(lower, "apply") || strings.Contains(lower, "create") {
				return "", fmt.Errorf("flag %q not allowed in read-only mode", flag)
			}
			args = append(args, flag)
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "kubectl", args...)
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

	if len(output) > 65536 {
		output = output[:65536] + "\n... (truncated)"
	}

	if err != nil {
		return output, fmt.Errorf("kubectl: %w", err)
	}
	return output, nil
}
