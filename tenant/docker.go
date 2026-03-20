package tenant

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DockerSandbox manages Docker containers for tenant isolation.
type DockerSandbox struct {
	image   string // default container image
	timeout time.Duration
}

// NewDockerSandbox creates a Docker sandbox manager.
func NewDockerSandbox(image string, timeout time.Duration) *DockerSandbox {
	if image == "" {
		image = "alpine:latest"
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &DockerSandbox{image: image, timeout: timeout}
}

// IsAvailable checks if Docker is accessible.
func (ds *DockerSandbox) IsAvailable() bool {
	cmd := exec.Command("docker", "info")
	return cmd.Run() == nil
}

// RunCommand executes a command inside a Docker container.
// Returns stdout, stderr, and any error.
func (ds *DockerSandbox) RunCommand(ctx context.Context, tenant, command string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, ds.timeout)
	defer cancel()

	containerName := fmt.Sprintf("cyntr-%s-%d", tenant, time.Now().UnixNano())

	args := []string{
		"run", "--rm",
		"--name", containerName,
		"--network", "none",           // no network by default
		"--memory", "256m",            // memory limit
		"--cpus", "0.5",               // CPU limit
		"--read-only",                 // read-only filesystem
		"--tmpfs", "/tmp:rw,size=64m", // writable /tmp
		ds.image,
		"sh", "-c", command,
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// RunCommandWithNetwork executes with network access to specific hosts.
func (ds *DockerSandbox) RunCommandWithNetwork(ctx context.Context, tenant, command string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, ds.timeout)
	defer cancel()

	containerName := fmt.Sprintf("cyntr-%s-%d", tenant, time.Now().UnixNano())

	args := []string{
		"run", "--rm",
		"--name", containerName,
		"--memory", "256m",
		"--cpus", "0.5",
		"--read-only",
		"--tmpfs", "/tmp:rw,size=64m",
		ds.image,
		"sh", "-c", command,
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// Cleanup removes any dangling containers for a tenant.
func (ds *DockerSandbox) Cleanup(tenant string) error {
	cmd := exec.Command("docker", "ps", "-aq", "--filter", fmt.Sprintf("name=cyntr-%s", tenant))
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return err
	}

	ids := strings.TrimSpace(out.String())
	if ids == "" {
		return nil
	}

	for _, id := range strings.Split(ids, "\n") {
		id = strings.TrimSpace(id)
		if id != "" {
			exec.Command("docker", "rm", "-f", id).Run()
		}
	}
	return nil
}
