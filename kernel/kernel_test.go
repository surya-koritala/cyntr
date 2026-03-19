package kernel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

type testModule struct {
	mu          sync.Mutex
	name        string
	deps        []string
	initCalled  bool
	startCalled bool
	stopCalled  bool
	healthy     bool
	initErr     error
	startErr    error
	stopErr     error
	initOrder   *[]string
}

func newTestModule(name string) *testModule {
	return &testModule{name: name, healthy: true}
}

func (m *testModule) Name() string           { return m.name }
func (m *testModule) Dependencies() []string { return m.deps }

func (m *testModule) Init(ctx context.Context, svc *Services) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initCalled = true
	if m.initOrder != nil {
		*m.initOrder = append(*m.initOrder, m.name)
	}
	return m.initErr
}

func (m *testModule) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalled = true
	return m.startErr
}

func (m *testModule) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalled = true
	return m.stopErr
}

func (m *testModule) Health(ctx context.Context) HealthStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return HealthStatus{Healthy: m.healthy, Message: "ok"}
}

func (m *testModule) wasInitCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.initCalled
}

func (m *testModule) wasStartCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.startCalled
}

func (m *testModule) wasStopCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopCalled
}

func TestKernelNew(t *testing.T) {
	k := New()
	if k == nil {
		t.Fatal("expected non-nil kernel")
	}
}

func TestKernelRegisterModule(t *testing.T) {
	k := New()
	mod := newTestModule("test")

	err := k.Register(mod)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	modules := k.Modules()
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if modules[0] != "test" {
		t.Fatalf("expected module name 'test', got '%s'", modules[0])
	}
}

func TestKernelRegisterDuplicate(t *testing.T) {
	k := New()
	mod1 := newTestModule("test")
	mod2 := newTestModule("test")

	if err := k.Register(mod1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err := k.Register(mod2)
	if err == nil {
		t.Fatal("expected error for duplicate module registration")
	}
}

func TestKernelStartStop(t *testing.T) {
	k := New()
	k.configLoaded = true
	mod := newTestModule("test")

	if err := k.Register(mod); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	if !mod.wasInitCalled() {
		t.Fatal("expected Init to be called")
	}
	if !mod.wasStartCalled() {
		t.Fatal("expected Start to be called")
	}

	if err := k.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	if !mod.wasStopCalled() {
		t.Fatal("expected Stop to be called")
	}
}

func TestKernelModuleState(t *testing.T) {
	k := New()
	k.configLoaded = true
	mod := newTestModule("test")

	if err := k.Register(mod); err != nil {
		t.Fatalf("register: %v", err)
	}

	state := k.ModuleState("test")
	if state != ModuleStateRegistered {
		t.Fatalf("expected registered, got %s", state)
	}

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	state = k.ModuleState("test")
	if state != ModuleStateRunning {
		t.Fatalf("expected running, got %s", state)
	}

	if err := k.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	state = k.ModuleState("test")
	if state != ModuleStateStopped {
		t.Fatalf("expected stopped, got %s", state)
	}
}

func TestKernelDependencyOrder(t *testing.T) {
	k := New()
	k.configLoaded = true
	order := make([]string, 0)

	modC := newTestModule("c")
	modC.deps = []string{"b"}
	modC.initOrder = &order

	modB := newTestModule("b")
	modB.deps = []string{"a"}
	modB.initOrder = &order

	modA := newTestModule("a")
	modA.initOrder = &order

	if err := k.Register(modC); err != nil {
		t.Fatalf("register c: %v", err)
	}
	if err := k.Register(modB); err != nil {
		t.Fatalf("register b: %v", err)
	}
	if err := k.Register(modA); err != nil {
		t.Fatalf("register a: %v", err)
	}

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer k.Stop(ctx)

	if len(order) != 3 {
		t.Fatalf("expected 3 init calls, got %d", len(order))
	}
	if order[0] != "a" {
		t.Fatalf("expected first init 'a', got '%s'", order[0])
	}
	if order[1] != "b" {
		t.Fatalf("expected second init 'b', got '%s'", order[1])
	}
	if order[2] != "c" {
		t.Fatalf("expected third init 'c', got '%s'", order[2])
	}
}

func TestKernelStopReverseOrder(t *testing.T) {
	k := New()
	k.configLoaded = true
	stopOrder := make([]string, 0)
	var mu sync.Mutex

	makeModule := func(name string, deps []string) *stopOrderModule {
		return &stopOrderModule{
			testModule: testModule{name: name, deps: deps, healthy: true},
			stopOrder:  &stopOrder,
			stopMu:     &mu,
		}
	}

	modA := makeModule("a", nil)
	modB := makeModule("b", []string{"a"})
	modC := makeModule("c", []string{"b"})

	k.Register(modA)
	k.Register(modB)
	k.Register(modC)

	ctx := context.Background()
	k.Start(ctx)
	k.Stop(ctx)

	if len(stopOrder) != 3 {
		t.Fatalf("expected 3 stop calls, got %d", len(stopOrder))
	}
	if stopOrder[0] != "c" {
		t.Fatalf("expected first stop 'c', got '%s'", stopOrder[0])
	}
	if stopOrder[1] != "b" {
		t.Fatalf("expected second stop 'b', got '%s'", stopOrder[1])
	}
	if stopOrder[2] != "a" {
		t.Fatalf("expected third stop 'a', got '%s'", stopOrder[2])
	}
}

type stopOrderModule struct {
	testModule
	stopOrder *[]string
	stopMu    *sync.Mutex
}

func (m *stopOrderModule) Stop(ctx context.Context) error {
	m.stopMu.Lock()
	*m.stopOrder = append(*m.stopOrder, m.name)
	m.stopMu.Unlock()
	return m.testModule.Stop(ctx)
}

func TestKernelCyclicDependencyError(t *testing.T) {
	k := New()
	k.configLoaded = true

	modA := newTestModule("a")
	modA.deps = []string{"b"}
	modB := newTestModule("b")
	modB.deps = []string{"a"}

	k.Register(modA)
	k.Register(modB)

	ctx := context.Background()
	err := k.Start(ctx)
	if err == nil {
		t.Fatal("expected error for cyclic dependency")
	}
}

func TestKernelMissingDependencyError(t *testing.T) {
	k := New()
	k.configLoaded = true
	mod := newTestModule("a")
	mod.deps = []string{"nonexistent"}

	k.Register(mod)

	ctx := context.Background()
	err := k.Start(ctx)
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
}

func TestKernelInitFailure(t *testing.T) {
	k := New()
	k.configLoaded = true
	mod := newTestModule("failing")
	mod.initErr = fmt.Errorf("init boom")

	if err := k.Register(mod); err != nil {
		t.Fatalf("register: %v", err)
	}

	ctx := context.Background()
	err := k.Start(ctx)
	if err == nil {
		t.Fatal("expected error when module init fails")
	}

	state := k.ModuleState("failing")
	if state != ModuleStateFailed {
		t.Fatalf("expected failed state, got %s", state)
	}
}

func TestKernelStartRefusesWithoutConfig(t *testing.T) {
	k := New()
	mod := newTestModule("test")
	k.Register(mod)

	ctx := context.Background()
	err := k.Start(ctx)
	if err == nil {
		t.Fatal("expected error when starting without config loaded")
	}
}

func TestKernelHealthReport(t *testing.T) {
	k := New()
	k.configLoaded = true

	healthy := newTestModule("healthy")
	unhealthy := newTestModule("unhealthy")
	unhealthy.healthy = false

	k.Register(healthy)
	k.Register(unhealthy)

	ctx := context.Background()
	k.Start(ctx)
	defer k.Stop(ctx)

	report := k.HealthReport(ctx)
	if len(report) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(report))
	}
	if !report["healthy"].Healthy {
		t.Fatal("expected 'healthy' module to be healthy")
	}
	if report["unhealthy"].Healthy {
		t.Fatal("expected 'unhealthy' module to be unhealthy")
	}
}

type ipcTestModule struct {
	name     string
	deps     []string
	healthy  bool
	bus      *ipc.Bus
	received chan string
}

func (m *ipcTestModule) Name() string           { return m.name }
func (m *ipcTestModule) Dependencies() []string { return m.deps }

func (m *ipcTestModule) Init(ctx context.Context, svc *Services) error {
	m.bus = svc.Bus
	return nil
}

func (m *ipcTestModule) Start(ctx context.Context) error {
	m.bus.Handle(m.name, "echo", func(msg ipc.Message) (ipc.Message, error) {
		if m.received != nil {
			m.received <- msg.Payload.(string)
		}
		return ipc.Message{
			Type:    ipc.MessageTypeResponse,
			Payload: "echo:" + msg.Payload.(string),
		}, nil
	})
	return nil
}

func (m *ipcTestModule) Stop(ctx context.Context) error { return nil }
func (m *ipcTestModule) Health(ctx context.Context) HealthStatus {
	return HealthStatus{Healthy: m.healthy}
}

func TestKernelIntegrationModulesIPCBus(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "cyntr.yaml")
	os.WriteFile(cfgPath, []byte("version: \"1\"\nlisten:\n  address: \"127.0.0.1:8080\"\n"), 0644)

	k := New()
	k.LoadConfig(cfgPath)

	received := make(chan string, 1)
	responder := &ipcTestModule{name: "responder", healthy: true, received: received}
	caller := &ipcTestModule{name: "caller", healthy: true}

	k.Register(responder)
	k.Register(caller)

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer k.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := caller.bus.Request(reqCtx, ipc.Message{
		Source:  "caller",
		Target:  "responder",
		Type:    ipc.MessageTypeRequest,
		Topic:   "echo",
		Payload: "hello",
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	if resp.Payload != "echo:hello" {
		t.Fatalf("expected 'echo:hello', got %v", resp.Payload)
	}

	select {
	case val := <-received:
		if val != "hello" {
			t.Fatalf("responder received %q, expected 'hello'", val)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for responder")
	}
}
