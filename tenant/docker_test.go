package tenant

import (
	"context"
	"testing"
	"time"
)

func TestDockerSandboxDefaults(t *testing.T) {
	ds := NewDockerSandbox("", 0)
	if ds.image != "alpine:latest" {
		t.Fatalf("got %q", ds.image)
	}
	if ds.timeout != 30*time.Second {
		t.Fatalf("got %v", ds.timeout)
	}
}

func TestDockerSandboxCustomImage(t *testing.T) {
	ds := NewDockerSandbox("ubuntu:22.04", 10*time.Second)
	if ds.image != "ubuntu:22.04" {
		t.Fatalf("got %q", ds.image)
	}
	if ds.timeout != 10*time.Second {
		t.Fatalf("got %v", ds.timeout)
	}
}

func TestDockerSandboxRunCommand(t *testing.T) {
	ds := NewDockerSandbox("alpine:latest", 30*time.Second)

	if !ds.IsAvailable() {
		t.Skip("Docker not available — skipping live test")
	}

	stdout, _, err := ds.RunCommand(context.Background(), "test", "echo hello from docker")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout != "hello from docker\n" {
		t.Fatalf("got %q", stdout)
	}
}

func TestDockerSandboxNoNetwork(t *testing.T) {
	ds := NewDockerSandbox("alpine:latest", 10*time.Second)

	if !ds.IsAvailable() {
		t.Skip("Docker not available")
	}

	// Try to ping — should fail with no network
	_, _, err := ds.RunCommand(context.Background(), "test", "ping -c1 -W1 8.8.8.8")
	if err == nil {
		t.Fatal("expected network failure")
	}
}

func TestDockerSandboxTimeout(t *testing.T) {
	ds := NewDockerSandbox("alpine:latest", 2*time.Second)

	if !ds.IsAvailable() {
		t.Skip("Docker not available")
	}

	_, _, err := ds.RunCommand(context.Background(), "test", "sleep 60")
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestDockerSandboxCleanup(t *testing.T) {
	ds := NewDockerSandbox("alpine:latest", 0)
	// Cleanup should not error even with no containers
	if err := ds.Cleanup("nonexistent"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}
