package tools

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/tenant"
)

func TestInProcessBackendRunHappy(t *testing.T) {
	b := InProcessBackend{}
	out, err := b.Run(context.Background(), "t1", "echo hi", 5*time.Second)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out) != "hi" {
		t.Fatalf("got %q", out)
	}
}

func TestInProcessBackendMergesStderr(t *testing.T) {
	b := InProcessBackend{}
	out, _ := b.Run(context.Background(), "t1", "echo out; echo err >&2", 5*time.Second)
	if !strings.Contains(out, "out") || !strings.Contains(out, "err") {
		t.Fatalf("expected both stdout and stderr in %q", out)
	}
}

func TestInProcessBackendTimeout(t *testing.T) {
	b := InProcessBackend{}
	_, err := b.Run(context.Background(), "t1", "sleep 60", 100*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestInProcessBackendExitCode(t *testing.T) {
	b := InProcessBackend{}
	_, err := b.Run(context.Background(), "t1", "exit 2", 5*time.Second)
	if err == nil {
		t.Fatal("expected non-zero exit error")
	}
}

func TestDockerBackendRun(t *testing.T) {
	sb := tenant.NewDockerSandbox("alpine:latest", 30*time.Second)
	if !sb.IsAvailable() {
		t.Skip("docker not available")
	}
	b := NewDockerBackend(sb)
	out, err := b.Run(context.Background(), "tenantA", "echo hello-docker", 30*time.Second)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "hello-docker") {
		t.Fatalf("got %q", out)
	}
}

func TestDockerBackendNilSandbox(t *testing.T) {
	var b *DockerBackend
	_, err := b.Run(context.Background(), "t1", "echo x", time.Second)
	if err == nil {
		t.Fatal("expected error for nil backend")
	}
}

func TestNewDockerBackendSelectorRoutes(t *testing.T) {
	policies := []ShellExecPolicy{
		{Tenant: "tA", Backend: "docker", Image: "alpine:latest", Timeout: 30 * time.Second},
		{Tenant: "tB", Backend: "inprocess"},
	}
	sel, err := NewDockerBackendSelector(policies)
	if err != nil {
		t.Fatalf("build selector: %v", err)
	}

	probe := tenant.NewDockerSandbox("", 0)
	dockerUp := probe.IsAvailable()

	// tA: docker if available, otherwise the global fallback to in-process.
	got := sel("tA")
	if dockerUp {
		if _, ok := got.(*DockerBackend); !ok {
			t.Fatalf("tA: expected *DockerBackend, got %T", got)
		}
	} else {
		if _, ok := got.(InProcessBackend); !ok {
			t.Fatalf("tA (no docker): expected InProcessBackend, got %T", got)
		}
	}

	// tB: explicit in-process.
	if _, ok := sel("tB").(InProcessBackend); !ok {
		t.Fatalf("tB: expected InProcessBackend, got %T", sel("tB"))
	}

	// Unknown tenant: defaults to in-process.
	if _, ok := sel("tZ").(InProcessBackend); !ok {
		t.Fatalf("unknown tenant: expected InProcessBackend, got %T", sel("tZ"))
	}
}

func TestNewDockerBackendSelectorSharesSandbox(t *testing.T) {
	probe := tenant.NewDockerSandbox("", 0)
	if !probe.IsAvailable() {
		t.Skip("docker not available")
	}
	policies := []ShellExecPolicy{
		{Tenant: "t1", Backend: "docker", Image: "alpine:latest", Timeout: 10 * time.Second},
		{Tenant: "t2", Backend: "docker", Image: "alpine:latest", Timeout: 10 * time.Second},
		{Tenant: "t3", Backend: "docker", Image: "alpine:latest", Timeout: 30 * time.Second},
	}
	sel, err := NewDockerBackendSelector(policies)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	b1 := sel("t1").(*DockerBackend)
	b2 := sel("t2").(*DockerBackend)
	b3 := sel("t3").(*DockerBackend)
	if b1.sb != b2.sb {
		t.Fatal("expected t1 and t2 to share a sandbox (same image+timeout)")
	}
	if b1.sb == b3.sb {
		t.Fatal("expected t3 to have a distinct sandbox (different timeout)")
	}
}

func TestNewDockerBackendSelectorUnknownBackend(t *testing.T) {
	_, err := NewDockerBackendSelector([]ShellExecPolicy{
		{Tenant: "tX", Backend: "wasm"},
	})
	if err == nil {
		t.Fatal("expected error for unknown backend")
	}
}

// fakeBackend records the (tenant, command) it was invoked with so we can
// assert ShellTool routes correctly without spinning up real processes.
type fakeBackend struct {
	name        string
	calls       int32
	lastTenant  string
	lastCommand string
}

func (f *fakeBackend) Run(_ context.Context, tenant, command string, _ time.Duration) (string, error) {
	atomic.AddInt32(&f.calls, 1)
	f.lastTenant = tenant
	f.lastCommand = command
	return f.name, nil
}

func TestShellToolRoutesViaSelector(t *testing.T) {
	dockerB := &fakeBackend{name: "docker"}
	inprocB := &fakeBackend{name: "inproc"}

	tool := &ShellTool{
		BackendSelector: func(tenant string) ShellBackend {
			if tenant == "tA" {
				return dockerB
			}
			return inprocB
		},
	}

	ctxA := agent.WithToolCaller(context.Background(), "tA", "agent1", "user1")
	outA, err := tool.Execute(ctxA, map[string]string{"command": "echo a"})
	if err != nil {
		t.Fatalf("execute tA: %v", err)
	}
	if outA != "docker" {
		t.Fatalf("tA: expected docker backend, got %q", outA)
	}
	if dockerB.lastTenant != "tA" || dockerB.lastCommand != "echo a" {
		t.Fatalf("docker backend got tenant=%q cmd=%q", dockerB.lastTenant, dockerB.lastCommand)
	}

	ctxB := agent.WithToolCaller(context.Background(), "tB", "agent2", "user2")
	outB, err := tool.Execute(ctxB, map[string]string{"command": "echo b"})
	if err != nil {
		t.Fatalf("execute tB: %v", err)
	}
	if outB != "inproc" {
		t.Fatalf("tB: expected inproc backend, got %q", outB)
	}
	if atomic.LoadInt32(&dockerB.calls) != 1 {
		t.Fatalf("docker backend should have been called exactly once, got %d", dockerB.calls)
	}
	if atomic.LoadInt32(&inprocB.calls) != 1 {
		t.Fatalf("inproc backend should have been called exactly once, got %d", inprocB.calls)
	}
}

func TestShellToolNilSelectorFallsBackInProcess(t *testing.T) {
	// Nil selector must preserve legacy behavior — Execute should run the
	// command in-process and return real output.
	tool := &ShellTool{}
	out, err := tool.Execute(context.Background(), map[string]string{"command": "echo legacy"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(out) != "legacy" {
		t.Fatalf("got %q", out)
	}
}

func TestShellToolNilSelectorWithTenantContextStillInProcess(t *testing.T) {
	// Even with a tenant set on the context, a nil selector must still route
	// in-process — guaranteeing backwards compat with existing deployments
	// that haven't opted into shell_exec_policies.
	tool := &ShellTool{}
	ctx := agent.WithToolCaller(context.Background(), "anyTenant", "a", "u")
	out, err := tool.Execute(ctx, map[string]string{"command": "echo compat"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(out) != "compat" {
		t.Fatalf("got %q", out)
	}
}

func TestShellToolSelectorReturningNilFallsBack(t *testing.T) {
	tool := &ShellTool{
		BackendSelector: func(string) ShellBackend { return nil },
	}
	out, err := tool.Execute(context.Background(), map[string]string{"command": "echo ok"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("got %q", out)
	}
}
