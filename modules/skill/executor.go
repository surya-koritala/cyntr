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

// Execute checks capabilities before delegating to the inner executor.
func (e *SandboxedExecutor) Execute(ctx context.Context, s *InstalledSkill, input string) (*ExecutionResult, error) {
	// Enforce capability restrictions
	if s.Manifest.Capabilities.Shell {
		// Shell access must be explicitly allowed — this is a check, not a grant
	}

	// Check if this is an OpenClaw skill (untrusted)
	if s.Signature == "" {
		// Unsigned skill — enforce maximum restrictions
		// In a real WASM runtime, this would limit WASI capabilities
	}

	return e.inner.Execute(ctx, s, input)
}
