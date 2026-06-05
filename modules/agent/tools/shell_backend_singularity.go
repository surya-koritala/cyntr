package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// SingularityBackend runs shell commands inside a Singularity/Apptainer
// container image, satisfying the ShellBackend interface. It shells out to the
// host `singularity` (or `apptainer`) binary.
type SingularityBackend struct {
	Image  string
	Binary string // "singularity" (default) or "apptainer"

	// runner is injectable so the exec path is testable without the real
	// binary; nil means run the host binary.
	runner func(ctx context.Context, name string, args ...string) (string, error)
}

// NewSingularityBackend builds a backend for the given image. binary defaults
// to "singularity" when empty.
func NewSingularityBackend(image, binary string) *SingularityBackend {
	return &SingularityBackend{Image: image, Binary: binary}
}

// singularityArgs builds the exec invocation: `exec <image> bash -c <command>`.
func singularityArgs(image, command string) []string {
	return []string{"exec", image, "bash", "-c", command}
}

// Run executes command inside the configured image.
func (b *SingularityBackend) Run(ctx context.Context, _ string, command string, timeout time.Duration) (string, error) {
	if b == nil || b.Image == "" {
		return "", fmt.Errorf("singularity backend: no image configured")
	}
	bin := b.Binary
	if bin == "" {
		bin = "singularity"
	}
	if timeout <= 0 {
		timeout = shellDefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := singularityArgs(b.Image, command)
	if b.runner != nil {
		return b.runner(runCtx, bin, args...)
	}

	if _, err := exec.LookPath(bin); err != nil {
		return "", fmt.Errorf("singularity backend: %q not found on host: %w", bin, err)
	}
	cmd := exec.CommandContext(runCtx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	output := truncateOutput(mergeOutput(stdout.String(), stderr.String()))
	if runErr != nil {
		return output, fmt.Errorf("command failed: %w", runErr)
	}
	return output, nil
}
