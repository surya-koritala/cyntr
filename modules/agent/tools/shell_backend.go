package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/tenant"
)

// maxShellOutputBytes caps merged stdout+stderr returned by any backend
// so a runaway command can't blow up the agent context window.
const maxShellOutputBytes = 65536

// ShellBackend is the pluggable execution target for ShellTool. The default
// in-process backend preserves historical behavior; alternative backends
// (e.g. Docker) provide per-tenant isolation without changing the tool API.
type ShellBackend interface {
	Run(ctx context.Context, tenant, command string, timeout time.Duration) (string, error)
}

// InProcessBackend executes the command directly on the host with `bash -c`.
// This is the legacy / default behavior of ShellTool.
type InProcessBackend struct{}

// Run executes command via bash -c on the local host.
func (InProcessBackend) Run(ctx context.Context, _ string, command string, timeout time.Duration) (string, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	shellName, shellArgs := shellInvocation(runtime.GOOS, command)
	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	output := mergeOutput(stdout.String(), stderr.String())
	output = truncateOutput(output)

	if runErr != nil {
		return output, fmt.Errorf("command failed: %w", runErr)
	}
	return output, nil
}

// DockerBackend routes the command through a tenant.DockerSandbox so each
// execution is isolated (no network, read-only fs, mem/cpu capped, tmpfs /tmp).
type DockerBackend struct {
	sb *tenant.DockerSandbox
}

// NewDockerBackend wraps an existing DockerSandbox.
func NewDockerBackend(sb *tenant.DockerSandbox) *DockerBackend {
	return &DockerBackend{sb: sb}
}

// Run executes command inside a sandboxed container scoped to the given tenant.
// The DockerSandbox enforces its own timeout; the supplied timeout is honored
// via the context so callers still get the upper bound they asked for.
func (b *DockerBackend) Run(ctx context.Context, tenantID, command string, timeout time.Duration) (string, error) {
	if b == nil || b.sb == nil {
		return "", fmt.Errorf("docker backend not initialized")
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	stdout, stderr, runErr := b.sb.RunCommand(ctx, tenantID, command)
	output := mergeOutput(stdout, stderr)
	output = truncateOutput(output)

	if runErr != nil {
		return output, fmt.Errorf("command failed: %w", runErr)
	}
	return output, nil
}

// mergeOutput mirrors ShellTool's historical stdout+stderr concatenation
// so swapping backends doesn't change the on-the-wire response format.
func mergeOutput(stdout, stderr string) string {
	output := stdout
	if stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += stderr
	}
	return output
}

// truncateOutput caps the merged output at maxShellOutputBytes.
func truncateOutput(output string) string {
	if len(output) > maxShellOutputBytes {
		return output[:maxShellOutputBytes] + "\n... (truncated)"
	}
	return output
}

// ShellExecPolicy declares which backend a given tenant should use.
// Backend is "inprocess" (default) or "docker". Image/Timeout only apply
// when Backend == "docker".
type ShellExecPolicy struct {
	Tenant  string        `yaml:"tenant"`
	Backend string        `yaml:"backend"`
	Image   string        `yaml:"image"`
	Timeout time.Duration `yaml:"timeout"`
}

// NewDockerBackendSelector builds a (tenant -> ShellBackend) selector closure
// from a list of policies. Tenants not present in the list fall through to
// InProcessBackend. Multiple tenants targeting the same (image, timeout) share
// a single DockerSandbox instance. If Docker is unavailable on the host at
// construction time, we log a warning and silently fall back to in-process
// for every tenant so startup doesn't crash.
func NewDockerBackendSelector(policies []ShellExecPolicy) (func(tenant string) ShellBackend, error) {
	// Check docker availability once up-front using a probe sandbox.
	probe := tenant.NewDockerSandbox("", 0)
	dockerAvailable := probe.IsAvailable()

	wantsDocker := false
	for _, p := range policies {
		if p.Backend == "docker" {
			wantsDocker = true
			break
		}
	}

	if wantsDocker && !dockerAvailable {
		log.Warn("docker backend requested but docker is not available — falling back to in-process for all tenants", nil)
		return func(string) ShellBackend { return InProcessBackend{} }, nil
	}

	type sandboxKey struct {
		image   string
		timeout time.Duration
	}
	sandboxes := make(map[sandboxKey]*tenant.DockerSandbox)
	tenantBackend := make(map[string]ShellBackend)

	for _, p := range policies {
		switch p.Backend {
		case "docker":
			key := sandboxKey{image: p.Image, timeout: p.Timeout}
			sb, ok := sandboxes[key]
			if !ok {
				sb = tenant.NewDockerSandbox(p.Image, p.Timeout)
				sandboxes[key] = sb
			}
			tenantBackend[p.Tenant] = NewDockerBackend(sb)
		case "", "inprocess":
			tenantBackend[p.Tenant] = InProcessBackend{}
		default:
			return nil, fmt.Errorf("shell_exec_policies: unknown backend %q for tenant %q", p.Backend, p.Tenant)
		}
	}

	return func(tenantID string) ShellBackend {
		if b, ok := tenantBackend[tenantID]; ok {
			return b
		}
		return InProcessBackend{}
	}, nil
}
