package skill

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// ExecutionResult holds the output of a skill handler execution.
type ExecutionResult struct {
	Output   string
	ExitCode int
	Duration time.Duration
	Error    error
}

// Executor runs skill handlers.
type Executor interface {
	// Execute runs a skill handler with the given input.
	Execute(ctx context.Context, skill *InstalledSkill, input string) (*ExecutionResult, error)
	// Name returns the executor name.
	Name() string
}

// ScriptExecutor runs skill handlers as shell scripts.
// This is a bridge until WASM (wazero) is integrated.
type ScriptExecutor struct {
	timeout time.Duration
}

// NewScriptExecutor creates a script-based skill executor.
func NewScriptExecutor(timeout time.Duration) *ScriptExecutor {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &ScriptExecutor{timeout: timeout}
}

func (e *ScriptExecutor) Name() string { return "script" }

// Execute runs a skill's handler script.
// Looks for an executable file at <skill.Path>/handlers/run.sh
// Passes input via stdin, captures stdout as output.
func (e *ScriptExecutor) Execute(ctx context.Context, s *InstalledSkill, input string) (*ExecutionResult, error) {
	if s == nil {
		return nil, fmt.Errorf("nil skill")
	}

	scriptPath := s.Path + "/handlers/run.sh"

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	start := time.Now()

	cmd := exec.CommandContext(ctx, "sh", scriptPath)
	cmd.Stdin = bytes.NewBufferString(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	duration := time.Since(start)

	result := &ExecutionResult{
		Output:   stdout.String(),
		Duration: duration,
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		result.Error = err
		// Include stderr in output on error
		if stderr.Len() > 0 {
			result.Output += "\n" + stderr.String()
		}
	}

	return result, nil
}

// SandboxedExecutor wraps an executor with capability enforcement.
type SandboxedExecutor struct {
	inner Executor
}

// NewSandboxedExecutor wraps an executor with capability checks.
func NewSandboxedExecutor(inner Executor) *SandboxedExecutor {
	return &SandboxedExecutor{inner: inner}
}

func (e *SandboxedExecutor) Name() string { return "sandboxed-" + e.inner.Name() }

// Execute enforces the skill's declared capability allowlist before delegating
// to the inner executor. The inner ScriptExecutor runs an arbitrary shell
// script, so it requires the Shell capability. A skill that does not declare
// Shell must fail closed rather than silently gaining shell access.
func (e *SandboxedExecutor) Execute(ctx context.Context, s *InstalledSkill, input string) (*ExecutionResult, error) {
	if s == nil {
		return nil, fmt.Errorf("nil skill")
	}

	// The script-backed runtime executes a shell handler, which is shell
	// access. Require the skill to have declared the Shell capability;
	// otherwise refuse to run (fail closed).
	if !s.Manifest.Capabilities.Shell {
		return nil, fmt.Errorf("skill %q denied: shell handler requires the 'shell' capability, which is not declared", s.Manifest.Name)
	}

	return e.inner.Execute(ctx, s, input)
}
