package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// shellDefaultTimeout is the upper bound for any single shell command,
// regardless of which backend runs it.
const shellDefaultTimeout = 120 * time.Second

// ShellTool executes shell commands. By default it runs `bash -c` in-process,
// but a BackendSelector can route specific tenants through an isolated
// backend (e.g. tenant.DockerSandbox) without changing the tool API.
type ShellTool struct {
	// BackendSelector picks the ShellBackend for a given tenant. If nil,
	// the tool falls back to InProcessBackend for every call — preserving
	// the legacy single-tenant behavior used by existing deployments.
	BackendSelector func(tenant string) ShellBackend
}

func (t *ShellTool) Name() string { return "shell_exec" }
func (t *ShellTool) Description() string {
	return "Execute a shell command via bash and return its output. Supports multi-line scripts, pipes, and bash features. AWS CLI, Azure CLI, gcloud, and other tools are available if installed."
}
func (t *ShellTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"command": {Type: "string", Description: "The bash command to execute", Required: true},
	}
}

func (t *ShellTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	command := input["command"]
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	tenant, _, _ := agent.ToolCaller(ctx)

	backend := t.selectBackend(tenant)
	return backend.Run(ctx, tenant, command, shellDefaultTimeout)
}

// selectBackend returns the backend for the given tenant. nil BackendSelector
// (the default) means "always in-process" — guaranteeing backwards compat
// with deployments that don't configure shell_exec_policies.
func (t *ShellTool) selectBackend(tenant string) ShellBackend {
	if t.BackendSelector == nil {
		return InProcessBackend{}
	}
	if b := t.BackendSelector(tenant); b != nil {
		return b
	}
	return InProcessBackend{}
}
