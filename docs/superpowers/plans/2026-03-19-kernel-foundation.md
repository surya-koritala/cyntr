# Kernel Foundation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Cyntr's modular kernel — the core runtime that boots modules, mediates all inter-module communication via an IPC bus, loads YAML configuration, and tracks resource usage per tenant.

**Architecture:** Microkernel pattern in Go. A thin `Kernel` struct owns a `Module` registry, an `IPC Bus` for all inter-module communication (request/reply + pub/sub with backpressure), a `ConfigStore` for YAML configuration with change subscriptions, and a `ResourceManager` for per-tenant resource tracking. Modules are compiled-in Go types implementing a `Module` interface with a `Dependencies()` method; the kernel boots them in topological dependency order. Signal handling (SIGINT/SIGTERM for shutdown, SIGHUP for hot config reload) is a kernel responsibility.

**Tech Stack:** Go 1.22+, YAML via `gopkg.in/yaml.v3`. No other external deps.

**Scope exclusions:** Federation Protocol is implemented in a separate plan (Plan 8). This plan builds the kernel foundation that federation plugs into.

**Spec:** `docs/superpowers/specs/2026-03-19-cyntr-enterprise-platform-design.md`

---

## File Structure

```
cyntr/
├── cmd/cyntr/main.go                    # Binary entrypoint, boots kernel
├── kernel/
│   ├── module.go                        # Module interface, ModuleState type
│   ├── kernel.go                        # Kernel struct, Register, Start, Stop, Health
│   ├── kernel_test.go                   # Kernel lifecycle + integration tests
│   ├── ipc/
│   │   ├── types.go                     # Message envelope, MessageType, errors
│   │   ├── bus.go                       # Bus: request/reply + pub/sub with backpressure
│   │   └── bus_test.go                  # Bus tests with real channels
│   ├── config/
│   │   ├── schema.go                    # Config types (CyntrConfig, TenantConfig, etc.)
│   │   ├── store.go                     # YAML loading, validation, subscriptions
│   │   └── store_test.go               # Config tests with real YAML files
│   └── resource/
│       ├── manager.go                   # Per-tenant resource tracking & enforcement
│       └── manager_test.go             # Resource manager tests (incl. concurrent)
├── go.mod
└── go.sum
```

---

## Chunk 1: Project Scaffold + Module Interface

### Task 1: Initialize Go Module and CLI Entrypoint

**Files:**
- Create: `go.mod`
- Create: `cmd/cyntr/main.go`

- [ ] **Step 1: Check for existing go.mod and initialize Go module**

Run:
```bash
cd /Users/suryakoritala/Cyntr
test -f go.mod && echo "go.mod exists, skipping init" || go mod init github.com/cyntr-dev/cyntr
```
Expected: `go.mod` created (or already exists)

- [ ] **Step 2: Create minimal main.go**

Create `cmd/cyntr/main.go`:
```go
package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: cyntr <command>")
		fmt.Fprintln(os.Stderr, "commands: start, version")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "version":
		fmt.Printf("cyntr v%s\n", version)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}
```

- [ ] **Step 3: Build and verify**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go build -o cyntr ./cmd/cyntr && ./cyntr version
```
Expected: `cyntr v0.1.0`

- [ ] **Step 4: Commit**

```bash
git add go.mod cmd/cyntr/main.go
git commit -m "feat: initialize Go module and minimal CLI entrypoint"
```

---

### Task 2: Define Module Interface with Dependencies

**Files:**
- Create: `kernel/module.go`
- Create: `kernel/module_test.go`

- [ ] **Step 1: Write tests for ModuleState and MessageType string methods**

Create `kernel/module_test.go`:
```go
package kernel

import "testing"

func TestModuleStateString(t *testing.T) {
	tests := []struct {
		state ModuleState
		want  string
	}{
		{ModuleStateRegistered, "registered"},
		{ModuleStateInitialized, "initialized"},
		{ModuleStateRunning, "running"},
		{ModuleStateStopped, "stopped"},
		{ModuleStateFailed, "failed"},
		{ModuleState(99), "unknown(99)"},
	}

	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.want {
			t.Errorf("ModuleState(%d).String() = %q, want %q", int(tt.state), got, tt.want)
		}
	}
}

func TestHealthStatusDefaults(t *testing.T) {
	h := HealthStatus{}
	if h.Healthy {
		t.Error("default HealthStatus should not be healthy")
	}
	if h.Message != "" {
		t.Error("default HealthStatus message should be empty")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/ -v -count=1
```
Expected: FAIL — types not defined

- [ ] **Step 3: Write the Module interface and supporting types**

Create `kernel/module.go`:
```go
package kernel

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel/config"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/resource"
)

// ModuleState represents the lifecycle state of a module.
type ModuleState int

const (
	ModuleStateRegistered ModuleState = iota
	ModuleStateInitialized
	ModuleStateRunning
	ModuleStateStopped
	ModuleStateFailed
)

func (s ModuleState) String() string {
	switch s {
	case ModuleStateRegistered:
		return "registered"
	case ModuleStateInitialized:
		return "initialized"
	case ModuleStateRunning:
		return "running"
	case ModuleStateStopped:
		return "stopped"
	case ModuleStateFailed:
		return "failed"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// HealthStatus represents the health of a module.
type HealthStatus struct {
	Healthy bool
	Message string
}

// Module is the interface that all Cyntr modules must implement.
type Module interface {
	// Name returns the unique name of this module (e.g., "policy", "audit").
	Name() string

	// Dependencies returns the names of modules that must be initialized before this one.
	// Return nil or empty slice if no dependencies.
	Dependencies() []string

	// Init initializes the module with access to kernel services.
	// Called once during kernel startup, before Start, after dependencies are initialized.
	Init(ctx context.Context, services *Services) error

	// Start begins the module's main loop. Called after all modules are initialized.
	// Must be non-blocking — start goroutines if needed and return.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the module.
	Stop(ctx context.Context) error

	// Health returns the current health status of the module.
	Health(ctx context.Context) HealthStatus
}

// Services provides access to kernel services during module initialization.
type Services struct {
	Bus       *ipc.Bus
	Config    *config.Store
	Resources *resource.Manager
}
```

Note: This will not compile yet because the `ipc`, `config`, and `resource` packages don't exist. We'll create stub files to satisfy the compiler.

- [ ] **Step 4: Create stub packages so the module interface compiles**

Create `kernel/ipc/types.go`:
```go
package ipc

// Bus is the in-process message bus for inter-module communication.
// Full implementation in a later task.
type Bus struct{}
```

Create `kernel/config/schema.go`:
```go
package config

// Store manages configuration loading and access.
// Full implementation in a later task.
type Store struct{}
```

Create `kernel/resource/manager.go`:
```go
package resource

// Manager tracks resource usage per tenant and enforces limits.
// Full implementation in a later task.
type Manager struct{}
```

- [ ] **Step 5: Run tests to verify they pass**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/ -v -count=1
```
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add kernel/module.go kernel/module_test.go kernel/ipc/types.go kernel/config/schema.go kernel/resource/manager.go
git commit -m "feat: define Module interface with Dependencies and kernel service types"
```

---

### Task 3: Implement Kernel with Dependency-Ordered Boot

**Files:**
- Create: `kernel/kernel.go`
- Create: `kernel/kernel_test.go` (extend existing)

- [ ] **Step 1: Write failing tests for kernel creation, registration, and dependency ordering**

Replace `kernel/kernel_test.go` (which currently only has module_test content — that file is `kernel/module_test.go`):

Create `kernel/kernel_test.go`:
```go
package kernel

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// testModule is a real module implementation for testing.
// All fields are protected by a mutex for concurrent safety.
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
	initOrder   *[]string // shared slice to track init order across modules
}

func newTestModule(name string) *testModule {
	return &testModule{name: name, healthy: true}
}

func (m *testModule) Name() string        { return m.name }
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

	// Module "b" depends on "a", "c" depends on "b".
	// Register in reverse order to prove dependency sorting works.
	modC := newTestModule("c")
	modC.deps = []string{"b"}
	modC.initOrder = &order

	modB := newTestModule("b")
	modB.deps = []string{"a"}
	modB.initOrder = &order

	modA := newTestModule("a")
	modA.initOrder = &order

	// Register in reverse dependency order
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

	// Verify init order: a, then b, then c
	if len(order) != 3 {
		t.Fatalf("expected 3 init calls, got %d", len(order))
	}
	if order[0] != "a" {
		t.Fatalf("expected first init to be 'a', got '%s'", order[0])
	}
	if order[1] != "b" {
		t.Fatalf("expected second init to be 'a', got '%s'", order[1])
	}
	if order[2] != "c" {
		t.Fatalf("expected third init to be 'c', got '%s'", order[2])
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

	// Stop order should be reverse of init: c, b, a
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
	k.configLoaded = true // bypass config check for this test

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/ -v -count=1
```
Expected: FAIL — `New`, `Register`, `Start`, `Stop`, etc. not defined

- [ ] **Step 3: Implement Kernel with topological sort and safe locking**

Create `kernel/kernel.go`:
```go
package kernel

import (
	"context"
	"fmt"
	"sync"

	"github.com/cyntr-dev/cyntr/kernel/config"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/resource"
)

// Kernel is the core runtime that manages module lifecycle and inter-module communication.
type Kernel struct {
	mu           sync.RWMutex
	modules      []moduleEntry
	moduleIndex  map[string]int // name -> index in modules slice
	services     *Services
	running      bool
	configLoaded bool
	bootOrder    []int // indices into modules, in dependency order
}

type moduleEntry struct {
	module Module
	state  ModuleState
}

// New creates a new Kernel instance with IPC bus and resource manager ready.
func New() *Kernel {
	bus := ipc.NewBus()
	resources := resource.NewManager()

	return &Kernel{
		modules:     make([]moduleEntry, 0),
		moduleIndex: make(map[string]int),
		services: &Services{
			Bus:       bus,
			Resources: resources,
		},
	}
}

// LoadConfig loads configuration from a YAML file and wires it into services.
func (k *Kernel) LoadConfig(path string) error {
	store, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	k.services.Config = store
	k.configLoaded = true
	return nil
}

// Register adds a module to the kernel. Must be called before Start.
func (k *Kernel) Register(mod Module) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	name := mod.Name()
	if _, exists := k.moduleIndex[name]; exists {
		return fmt.Errorf("module %q already registered", name)
	}

	idx := len(k.modules)
	k.modules = append(k.modules, moduleEntry{
		module: mod,
		state:  ModuleStateRegistered,
	})
	k.moduleIndex[name] = idx
	return nil
}

// Modules returns the names of all registered modules in registration order.
func (k *Kernel) Modules() []string {
	k.mu.RLock()
	defer k.mu.RUnlock()

	names := make([]string, len(k.modules))
	for i, entry := range k.modules {
		names[i] = entry.module.Name()
	}
	return names
}

// ModuleState returns the current state of a module by name.
func (k *Kernel) ModuleState(name string) ModuleState {
	k.mu.RLock()
	defer k.mu.RUnlock()

	if idx, ok := k.moduleIndex[name]; ok {
		return k.modules[idx].state
	}
	return ModuleStateFailed
}

// Start initializes and starts all registered modules in dependency order.
// The kernel must have LoadConfig called first, or Start returns an error.
func (k *Kernel) Start(ctx context.Context) error {
	k.mu.Lock()

	if !k.configLoaded {
		k.mu.Unlock()
		return fmt.Errorf("config not loaded: call LoadConfig before Start")
	}

	// Compute boot order via topological sort
	order, err := k.topoSort()
	if err != nil {
		k.mu.Unlock()
		return err
	}
	k.bootOrder = order

	// Capture what we need, then release the lock before calling module methods.
	// This prevents deadlocks if modules call kernel methods during Init/Start.
	entries := make([]int, len(order))
	copy(entries, order)
	services := k.services
	k.mu.Unlock()

	// Phase 1: Initialize all modules in dependency order
	for _, idx := range entries {
		k.mu.RLock()
		mod := k.modules[idx].module
		k.mu.RUnlock()

		if err := mod.Init(ctx, services); err != nil {
			k.mu.Lock()
			k.modules[idx].state = ModuleStateFailed
			k.mu.Unlock()
			return fmt.Errorf("module %q init failed: %w", mod.Name(), err)
		}

		k.mu.Lock()
		k.modules[idx].state = ModuleStateInitialized
		k.mu.Unlock()
	}

	// Phase 2: Start all modules in dependency order
	for _, idx := range entries {
		k.mu.RLock()
		mod := k.modules[idx].module
		k.mu.RUnlock()

		if err := mod.Start(ctx); err != nil {
			k.mu.Lock()
			k.modules[idx].state = ModuleStateFailed
			k.mu.Unlock()
			return fmt.Errorf("module %q start failed: %w", mod.Name(), err)
		}

		k.mu.Lock()
		k.modules[idx].state = ModuleStateRunning
		k.mu.Unlock()
	}

	k.mu.Lock()
	k.running = true
	k.mu.Unlock()

	return nil
}

// Stop gracefully shuts down all running modules in reverse dependency order.
func (k *Kernel) Stop(ctx context.Context) error {
	k.mu.Lock()
	order := make([]int, len(k.bootOrder))
	copy(order, k.bootOrder)
	k.mu.Unlock()

	var firstErr error

	// Stop in reverse dependency order
	for i := len(order) - 1; i >= 0; i-- {
		idx := order[i]

		k.mu.RLock()
		entry := k.modules[idx]
		k.mu.RUnlock()

		if entry.state != ModuleStateRunning {
			continue
		}

		if err := entry.module.Stop(ctx); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("module %q stop failed: %w", entry.module.Name(), err)
			}
			k.mu.Lock()
			k.modules[idx].state = ModuleStateFailed
			k.mu.Unlock()
		} else {
			k.mu.Lock()
			k.modules[idx].state = ModuleStateStopped
			k.mu.Unlock()
		}
	}

	k.mu.Lock()
	k.running = false
	k.mu.Unlock()

	return firstErr
}

// HealthReport runs health checks on all running modules and returns results.
func (k *Kernel) HealthReport(ctx context.Context) map[string]HealthStatus {
	k.mu.RLock()
	// Collect running modules
	type moduleInfo struct {
		name   string
		module Module
	}
	var running []moduleInfo
	for _, entry := range k.modules {
		if entry.state == ModuleStateRunning {
			running = append(running, moduleInfo{
				name:   entry.module.Name(),
				module: entry.module,
			})
		}
	}
	k.mu.RUnlock()

	report := make(map[string]HealthStatus, len(running))
	for _, m := range running {
		report[m.name] = m.module.Health(ctx)
	}
	return report
}

// topoSort returns module indices in topological (dependency) order.
// Must be called with k.mu held.
func (k *Kernel) topoSort() ([]int, error) {
	n := len(k.modules)
	inDegree := make([]int, n)
	adj := make([][]int, n) // adj[i] = list of modules that depend on i

	for i, entry := range k.modules {
		for _, dep := range entry.module.Dependencies() {
			depIdx, ok := k.moduleIndex[dep]
			if !ok {
				return nil, fmt.Errorf("module %q depends on unregistered module %q", entry.module.Name(), dep)
			}
			adj[depIdx] = append(adj[depIdx], i)
			inDegree[i]++
		}
	}

	// Kahn's algorithm
	queue := make([]int, 0)
	for i := 0; i < n; i++ {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}

	var order []int
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		order = append(order, curr)

		for _, next := range adj[curr] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(order) != n {
		return nil, fmt.Errorf("cyclic dependency detected among modules")
	}

	return order, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/ -v -count=1 -race
```
Expected: All tests PASS, no race conditions

- [ ] **Step 5: Commit**

```bash
git add kernel/kernel.go kernel/kernel_test.go
git commit -m "feat: implement Kernel with dependency-ordered boot, reverse-order stop, and safe locking"
```

---

## Chunk 2: IPC Bus with Backpressure

### Task 4: Define IPC Message Types

**Files:**
- Replace: `kernel/ipc/types.go` (was a stub)
- Create: `kernel/ipc/types_test.go`

- [ ] **Step 1: Write tests for message type strings**

Create `kernel/ipc/types_test.go`:
```go
package ipc

import "testing"

func TestMessageTypeString(t *testing.T) {
	tests := []struct {
		mt   MessageType
		want string
	}{
		{MessageTypeRequest, "request"},
		{MessageTypeResponse, "response"},
		{MessageTypeEvent, "event"},
		{MessageType(99), "unknown(99)"},
	}

	for _, tt := range tests {
		got := tt.mt.String()
		if got != tt.want {
			t.Errorf("MessageType(%d).String() = %q, want %q", int(tt.mt), got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/ipc/ -v -count=1
```
Expected: FAIL — `MessageType` and `String()` not defined (stub only has `Bus` struct)

- [ ] **Step 3: Write full IPC types**

Replace `kernel/ipc/types.go`:
```go
package ipc

import (
	"errors"
	"fmt"
	"time"
)

// MessageType distinguishes between request/reply and event messages.
type MessageType int

const (
	MessageTypeRequest MessageType = iota
	MessageTypeResponse
	MessageTypeEvent
)

func (t MessageType) String() string {
	switch t {
	case MessageTypeRequest:
		return "request"
	case MessageTypeResponse:
		return "response"
	case MessageTypeEvent:
		return "event"
	default:
		return fmt.Sprintf("unknown(%d)", int(t))
	}
}

// Message is the envelope for all inter-module communication.
type Message struct {
	ID       string      // Unique request ID
	Source   string      // Source module name
	Target   string      // Target module name, or "*" for broadcast events
	Type     MessageType // Request, Response, or Event
	Topic    string      // Message topic (e.g., "policy.check", "audit.write")
	Payload  any         // Typed payload
	Deadline time.Time   // From context.Context
	TraceID  string      // For distributed tracing
}

// IPC bus errors.
var (
	ErrModuleOverloaded = errors.New("ipc: target module overloaded")
	ErrModuleNotFound   = errors.New("ipc: target module not found")
	ErrTimeout          = errors.New("ipc: request timed out")
	ErrBusClosed        = errors.New("ipc: bus is closed")
	ErrNoHandler        = errors.New("ipc: no handler registered for topic")
)

// Handler processes incoming messages.
// For requests, return a response Message. For events, return value is ignored.
type Handler func(msg Message) (Message, error)

// Subscription represents an active event subscription.
type Subscription struct {
	Topic    string
	Module   string
	cancelFn func()
}

// Cancel removes this subscription from the bus.
func (s *Subscription) Cancel() {
	if s.cancelFn != nil {
		s.cancelFn()
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/ipc/ -v -count=1
```
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add kernel/ipc/types.go kernel/ipc/types_test.go
git commit -m "feat: define IPC message types, errors, and handler interfaces"
```

---

### Task 5: Implement IPC Bus Request/Reply with Backpressure

**Files:**
- Create: `kernel/ipc/bus.go`
- Create: `kernel/ipc/bus_test.go` (request/reply tests)

- [ ] **Step 1: Write failing tests for request/reply and backpressure**

Create `kernel/ipc/bus_test.go`:
```go
package ipc

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestBusNewAndClose(t *testing.T) {
	bus := NewBus()
	if bus == nil {
		t.Fatal("expected non-nil bus")
	}
	bus.Close()
}

func TestBusRequestReply(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	bus.Handle("responder", "echo", func(msg Message) (Message, error) {
		return Message{
			Type:    MessageTypeResponse,
			Payload: msg.Payload,
		}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := bus.Request(ctx, Message{
		Source:  "caller",
		Target:  "responder",
		Type:    MessageTypeRequest,
		Topic:   "echo",
		Payload: "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Payload != "hello" {
		t.Fatalf("expected payload 'hello', got %v", resp.Payload)
	}
}

func TestBusRequestTimeout(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	bus.Handle("slow", "work", func(msg Message) (Message, error) {
		time.Sleep(5 * time.Second)
		return Message{}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := bus.Request(ctx, Message{
		Source: "caller",
		Target: "slow",
		Type:   MessageTypeRequest,
		Topic:  "work",
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestBusRequestNoHandler(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	ctx := context.Background()
	_, err := bus.Request(ctx, Message{
		Source: "caller",
		Target: "nonexistent",
		Topic:  "anything",
	})
	if err != ErrNoHandler {
		t.Fatalf("expected ErrNoHandler, got %v", err)
	}
}

func TestBusRequestAfterClose(t *testing.T) {
	bus := NewBus()
	bus.Close()

	ctx := context.Background()
	_, err := bus.Request(ctx, Message{
		Source: "caller",
		Target: "any",
		Topic:  "any",
	})
	if err != ErrBusClosed {
		t.Fatalf("expected ErrBusClosed, got %v", err)
	}
}

func TestBusBackpressureOverloaded(t *testing.T) {
	bus := NewBusWithBufferSize(2) // tiny buffer for testing
	defer bus.Close()

	// Register a handler that blocks forever
	blocker := make(chan struct{})
	defer close(blocker)

	bus.Handle("slow", "work", func(msg Message) (Message, error) {
		<-blocker
		return Message{}, nil
	})

	// Fill the handler's inbound buffer
	// Send requests that will queue up
	var overloaded atomic.Bool
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := bus.Request(ctx, Message{
			Source: "caller",
			Target: "slow",
			Topic:  "work",
		})
		cancel()
		if err == ErrModuleOverloaded {
			overloaded.Store(true)
			break
		}
	}

	if !overloaded.Load() {
		t.Fatal("expected ErrModuleOverloaded when buffer is full")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/ipc/ -run TestBus -v -count=1
```
Expected: FAIL — `NewBus`, `NewBusWithBufferSize`, `Handle`, `Request` not defined

- [ ] **Step 3: Implement IPC bus with backpressure**

Create `kernel/ipc/bus.go`:
```go
package ipc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

const defaultBufferSize = 1024

// Bus is the in-process message bus for inter-module communication.
type Bus struct {
	mu          sync.RWMutex
	handlers    map[string]map[string]handlerEntry // module -> topic -> handler
	subscribers map[string][]subscriberEntry       // topic -> subscribers
	closed      bool
	bufferSize  int
}

type handlerEntry struct {
	handler Handler
	inbox   chan requestEnvelope
}

type requestEnvelope struct {
	msg    Message
	replyCh chan replyEnvelope
}

type replyEnvelope struct {
	msg Message
	err error
}

type subscriberEntry struct {
	module  string
	handler Handler
	id      string
}

// NewBus creates a new IPC bus with default buffer size (1024).
func NewBus() *Bus {
	return NewBusWithBufferSize(defaultBufferSize)
}

// NewBusWithBufferSize creates a new IPC bus with a specified buffer size.
func NewBusWithBufferSize(bufferSize int) *Bus {
	return &Bus{
		handlers:    make(map[string]map[string]handlerEntry),
		subscribers: make(map[string][]subscriberEntry),
		bufferSize:  bufferSize,
	}
}

// Handle registers a request handler for a module+topic pair.
// Incoming requests are buffered; if the buffer is full, callers get ErrModuleOverloaded.
func (b *Bus) Handle(module, topic string, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.handlers[module] == nil {
		b.handlers[module] = make(map[string]handlerEntry)
	}

	entry := handlerEntry{
		handler: h,
		inbox:   make(chan requestEnvelope, b.bufferSize),
	}
	b.handlers[module][topic] = entry

	// Start a goroutine to drain the inbox and process requests
	go func() {
		for req := range entry.inbox {
			resp, err := entry.handler(req.msg)
			if err != nil {
				req.replyCh <- replyEnvelope{err: err}
			} else {
				resp.ID = req.msg.ID
				resp.Source = req.msg.Target
				resp.Target = req.msg.Source
				req.replyCh <- replyEnvelope{msg: resp}
			}
		}
	}()
}

// Request sends a synchronous request and waits for a reply.
// Returns ErrModuleOverloaded if the target's buffer is full.
// Returns ErrNoHandler if no handler is registered.
// Respects context cancellation and deadlines.
func (b *Bus) Request(ctx context.Context, msg Message) (Message, error) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return Message{}, ErrBusClosed
	}

	topics, ok := b.handlers[msg.Target]
	if !ok {
		b.mu.RUnlock()
		return Message{}, ErrNoHandler
	}

	entry, ok := topics[msg.Topic]
	if !ok {
		b.mu.RUnlock()
		return Message{}, ErrNoHandler
	}
	b.mu.RUnlock()

	if msg.ID == "" {
		msg.ID = generateID()
	}
	if deadline, ok := ctx.Deadline(); ok {
		msg.Deadline = deadline
	}

	replyCh := make(chan replyEnvelope, 1)
	env := requestEnvelope{msg: msg, replyCh: replyCh}

	// Try to enqueue — non-blocking. If buffer is full, return overloaded.
	select {
	case entry.inbox <- env:
		// Enqueued successfully
	default:
		return Message{}, ErrModuleOverloaded
	}

	// Wait for reply or context cancellation
	select {
	case reply := <-replyCh:
		return reply.msg, reply.err
	case <-ctx.Done():
		return Message{}, ErrTimeout
	}
}

// Close shuts down the bus. Closes all handler inboxes.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for _, topics := range b.handlers {
		for _, entry := range topics {
			close(entry.inbox)
		}
	}
}

func generateID() string {
	buf := make([]byte, 8)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/ipc/ -run TestBus -v -count=1 -race
```
Expected: All tests PASS, no race conditions

- [ ] **Step 5: Commit**

```bash
git add kernel/ipc/bus.go kernel/ipc/bus_test.go
git commit -m "feat: implement IPC bus request/reply with buffered backpressure"
```

---

### Task 6: Implement IPC Pub/Sub

**Files:**
- Modify: `kernel/ipc/bus.go` (Publish method already stubbed via subscriberEntry)
- Modify: `kernel/ipc/bus_test.go` (add pub/sub tests)

- [ ] **Step 1: Write failing pub/sub tests**

Add to `kernel/ipc/bus_test.go`:
```go
func TestBusPubSub(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	received := make(chan string, 1)

	sub := bus.Subscribe("listener", "events.test", func(msg Message) (Message, error) {
		received <- msg.Payload.(string)
		return Message{}, nil
	})
	defer sub.Cancel()

	err := bus.Publish(Message{
		Source:  "emitter",
		Target:  "*",
		Type:    MessageTypeEvent,
		Topic:   "events.test",
		Payload: "data",
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case val := <-received:
		if val != "data" {
			t.Fatalf("expected 'data', got %q", val)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestBusMultipleSubscribers(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	var count atomic.Int32
	done := make(chan struct{}, 3)

	for i := 0; i < 3; i++ {
		name := string(rune('a' + i))
		sub := bus.Subscribe(name, "broadcast", func(msg Message) (Message, error) {
			count.Add(1)
			done <- struct{}{}
			return Message{}, nil
		})
		defer sub.Cancel()
	}

	bus.Publish(Message{
		Source: "emitter",
		Target: "*",
		Type:   MessageTypeEvent,
		Topic:  "broadcast",
	})

	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for subscriber %d", i)
		}
	}

	if count.Load() != 3 {
		t.Fatalf("expected 3 events, got %d", count.Load())
	}
}

func TestBusSubscriptionCancel(t *testing.T) {
	bus := NewBus()
	defer bus.Close()

	delivered := make(chan struct{}, 1)

	sub := bus.Subscribe("listener", "events.cancel", func(msg Message) (Message, error) {
		delivered <- struct{}{}
		return Message{}, nil
	})

	sub.Cancel()

	bus.Publish(Message{
		Source: "emitter",
		Target: "*",
		Type:   MessageTypeEvent,
		Topic:  "events.cancel",
	})

	// Use a deterministic check: try to receive, expect nothing
	select {
	case <-delivered:
		t.Fatal("received event after cancel — subscription was not removed")
	case <-time.After(200 * time.Millisecond):
		// Expected: no delivery after cancel.
		// Note: 200ms is generous. The bus dispatches events via goroutine,
		// so if cancel worked, there's no goroutine to deliver.
	}
}

func TestBusPublishAfterClose(t *testing.T) {
	bus := NewBus()
	bus.Close()

	err := bus.Publish(Message{
		Source: "emitter",
		Target: "*",
		Type:   MessageTypeEvent,
		Topic:  "test",
	})
	if err != ErrBusClosed {
		t.Fatalf("expected ErrBusClosed, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify pub/sub tests fail**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/ipc/ -run "TestBusPub|TestBusMultiple|TestBusSubscription|TestBusPublishAfter" -v -count=1
```
Expected: FAIL — `Subscribe` and `Publish` not defined

- [ ] **Step 3: Add Subscribe and Publish methods to bus.go**

Add to `kernel/ipc/bus.go` (before the `Close` method):
```go
// Subscribe registers an event handler for a topic.
// Multiple modules can subscribe to the same topic.
func (b *Bus) Subscribe(module, topic string, h Handler) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := generateID()
	b.subscribers[topic] = append(b.subscribers[topic], subscriberEntry{
		module:  module,
		handler: h,
		id:      id,
	})

	return &Subscription{
		Topic:  topic,
		Module: module,
		cancelFn: func() {
			b.unsubscribe(topic, id)
		},
	}
}

// Publish sends an event to all subscribers of the message's topic.
// Known limitation: pub/sub does not have per-subscriber backpressure.
// Events are dispatched via goroutines; slow subscribers block their goroutine
// but do not affect other subscribers. Buffered subscriber delivery will be
// added in a future iteration when audit logging needs it.
func (b *Bus) Publish(msg Message) error {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return ErrBusClosed
	}

	subs := make([]subscriberEntry, len(b.subscribers[msg.Topic]))
	copy(subs, b.subscribers[msg.Topic])
	b.mu.RUnlock()

	if msg.ID == "" {
		msg.ID = generateID()
	}

	for _, sub := range subs {
		sub := sub
		go func() {
			sub.handler(msg)
		}()
	}

	return nil
}

func (b *Bus) unsubscribe(topic, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[topic]
	for i, sub := range subs {
		if sub.id == id {
			b.subscribers[topic] = append(subs[:i], subs[i+1:]...)
			return
		}
	}
}
```

- [ ] **Step 4: Run all IPC tests**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/ipc/ -v -count=1 -race
```
Expected: All tests PASS

- [ ] **Step 5: Commit**

```bash
git add kernel/ipc/bus.go kernel/ipc/bus_test.go
git commit -m "feat: add pub/sub with subscription cancellation to IPC bus"
```

---

## Chunk 3: Config Store

### Task 7: Implement Config Schema and Store

**Files:**
- Replace: `kernel/config/schema.go` (was a stub)
- Create: `kernel/config/store.go`
- Create: `kernel/config/store_test.go`

- [ ] **Step 1: Write failing tests for config store**

Create `kernel/config/store_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func writeTestConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "cyntr.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestStoreLoadValid(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `
version: "1"
listen:
  address: "127.0.0.1:8080"
  webui: ":7700"
tenants:
  marketing:
    isolation: namespace
  finance:
    isolation: process
    cgroup:
      memory_limit: "2GB"
      cpu_shares: 512
`)

	store, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	cfg := store.Get()
	if cfg.Version != "1" {
		t.Fatalf("expected version '1', got '%s'", cfg.Version)
	}
	if cfg.Listen.Address != "127.0.0.1:8080" {
		t.Fatalf("expected address '127.0.0.1:8080', got '%s'", cfg.Listen.Address)
	}
	if len(cfg.Tenants) != 2 {
		t.Fatalf("expected 2 tenants, got %d", len(cfg.Tenants))
	}

	finance := cfg.Tenants["finance"]
	if finance.Isolation != "process" {
		t.Fatalf("expected finance isolation 'process', got '%s'", finance.Isolation)
	}
	if finance.Cgroup.MemoryLimit != "2GB" {
		t.Fatalf("expected memory limit '2GB', got '%s'", finance.Cgroup.MemoryLimit)
	}
}

func TestStoreLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/cyntr.yaml")
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestStoreLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `{{{not yaml`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestStoreReloadNotifiesListeners(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, `
version: "1"
listen:
  address: "127.0.0.1:8080"
`)

	store, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	var notified atomic.Int32
	store.OnChange(func(cfg CyntrConfig) {
		notified.Add(1)
	})

	writeTestConfig(t, dir, `
version: "2"
listen:
  address: "127.0.0.1:9090"
`)

	if err := store.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if notified.Load() != 1 {
		t.Fatalf("expected 1 notification, got %d", notified.Load())
	}

	cfg := store.Get()
	if cfg.Version != "2" {
		t.Fatalf("expected version '2' after reload, got '%s'", cfg.Version)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Version != "1" {
		t.Fatalf("expected default version '1', got '%s'", cfg.Version)
	}
	if cfg.Listen.Address != "127.0.0.1:8080" {
		t.Fatalf("expected default address, got '%s'", cfg.Listen.Address)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/config/ -v -count=1
```
Expected: FAIL — `Load`, `Get`, `OnChange`, `Reload`, `DefaultConfig` not defined

- [ ] **Step 3: Add yaml dependency**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go get gopkg.in/yaml.v3
```
Expected: `go.mod` and `go.sum` updated

- [ ] **Step 4: Write full config schema**

Replace `kernel/config/schema.go`:
```go
package config

// CyntrConfig is the root configuration structure loaded from cyntr.yaml.
type CyntrConfig struct {
	Version    string                  `yaml:"version"`
	Listen     ListenConfig            `yaml:"listen"`
	Tenants    map[string]TenantConfig `yaml:"tenants"`
	Auth       AuthConfig              `yaml:"auth"`
	Audit      AuditConfig             `yaml:"audit"`
	Federation FederationConfig        `yaml:"federation"`
}

// ListenConfig defines the network listener settings.
type ListenConfig struct {
	Address string `yaml:"address"`
	WebUI   string `yaml:"webui"`
}

// TenantConfig defines settings for a single tenant.
type TenantConfig struct {
	Isolation string       `yaml:"isolation"`
	Cgroup    CgroupConfig `yaml:"cgroup"`
	Policy    string       `yaml:"policy"`
}

// CgroupConfig defines resource limits for process-isolated tenants.
type CgroupConfig struct {
	MemoryLimit string `yaml:"memory_limit"`
	CPUShares   int    `yaml:"cpu_shares"`
}

// AuthConfig defines authentication settings.
type AuthConfig struct {
	Provider string `yaml:"provider"`
	Issuer   string `yaml:"issuer"`
	ClientID string `yaml:"client_id"`
}

// AuditConfig defines audit logging settings.
type AuditConfig struct {
	StoragePath string `yaml:"storage_path"`
	Retention   string `yaml:"retention"`
}

// FederationConfig defines federation settings.
type FederationConfig struct {
	Enabled bool         `yaml:"enabled"`
	Peers   []PeerConfig `yaml:"peers"`
}

// PeerConfig defines a federation peer.
type PeerConfig struct {
	Name        string `yaml:"name"`
	Endpoint    string `yaml:"endpoint"`
	Fingerprint string `yaml:"fingerprint"`
}
```

- [ ] **Step 5: Implement config store**

Create `kernel/config/store.go`:
```go
package config

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// Store manages configuration loading, access, and change notifications.
type Store struct {
	mu        sync.RWMutex
	path      string
	config    CyntrConfig
	listeners []func(CyntrConfig)
}

// DefaultConfig returns a CyntrConfig with sensible defaults.
func DefaultConfig() CyntrConfig {
	return CyntrConfig{
		Version: "1",
		Listen: ListenConfig{
			Address: "127.0.0.1:8080",
			WebUI:   ":7700",
		},
		Tenants: make(map[string]TenantConfig),
	}
}

// Load reads a YAML config file and returns a Store.
func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg CyntrConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	return &Store{
		path:   path,
		config: cfg,
	}, nil
}

// Get returns the current configuration.
func (s *Store) Get() CyntrConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// OnChange registers a callback invoked on config reload.
func (s *Store) OnChange(fn func(CyntrConfig)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

// Reload re-reads the config file and notifies listeners.
func (s *Store) Reload() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var cfg CyntrConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	s.mu.Lock()
	s.config = cfg
	listeners := make([]func(CyntrConfig), len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.Unlock()

	for _, fn := range listeners {
		fn(cfg)
	}

	return nil
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/config/ -v -count=1 -race
```
Expected: All tests PASS

- [ ] **Step 7: Commit**

```bash
git add kernel/config/schema.go kernel/config/store.go kernel/config/store_test.go go.mod go.sum
git commit -m "feat: implement config store with YAML loading and change subscriptions"
```

---

## Chunk 4: Resource Manager + CLI + Signal Handling

### Task 8: Implement Resource Manager with Concurrent Tests

**Files:**
- Replace: `kernel/resource/manager.go` (was a stub)
- Create: `kernel/resource/manager_test.go`

- [ ] **Step 1: Write failing tests including concurrent access**

Create `kernel/resource/manager_test.go`:
```go
package resource

import (
	"sync"
	"testing"
)

func TestManagerTrackGoroutines(t *testing.T) {
	m := NewManager()
	m.SetLimit("finance", ResourceGoroutines, 10)

	if err := m.Acquire("finance", ResourceGoroutines); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	usage := m.Usage("finance", ResourceGoroutines)
	if usage != 1 {
		t.Fatalf("expected usage 1, got %d", usage)
	}

	m.Release("finance", ResourceGoroutines)
	usage = m.Usage("finance", ResourceGoroutines)
	if usage != 0 {
		t.Fatalf("expected usage 0, got %d", usage)
	}
}

func TestManagerEnforceLimit(t *testing.T) {
	m := NewManager()
	m.SetLimit("finance", ResourceGoroutines, 2)

	m.Acquire("finance", ResourceGoroutines)
	m.Acquire("finance", ResourceGoroutines)

	err := m.Acquire("finance", ResourceGoroutines)
	if err != ErrLimitExceeded {
		t.Fatalf("expected ErrLimitExceeded, got %v", err)
	}
}

func TestManagerNoLimit(t *testing.T) {
	m := NewManager()

	for i := 0; i < 100; i++ {
		if err := m.Acquire("marketing", ResourceGoroutines); err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}

	if m.Usage("marketing", ResourceGoroutines) != 100 {
		t.Fatal("expected usage 100")
	}
}

func TestManagerMultipleTenants(t *testing.T) {
	m := NewManager()
	m.SetLimit("finance", ResourceGoroutines, 5)
	m.SetLimit("marketing", ResourceGoroutines, 3)

	for i := 0; i < 3; i++ {
		m.Acquire("finance", ResourceGoroutines)
		m.Acquire("marketing", ResourceGoroutines)
	}

	// Marketing at limit
	if err := m.Acquire("marketing", ResourceGoroutines); err != ErrLimitExceeded {
		t.Fatalf("expected ErrLimitExceeded for marketing, got %v", err)
	}

	// Finance still has room
	if err := m.Acquire("finance", ResourceGoroutines); err != nil {
		t.Fatalf("finance should have room: %v", err)
	}
}

func TestManagerSnapshot(t *testing.T) {
	m := NewManager()
	m.SetLimit("finance", ResourceGoroutines, 10)
	m.Acquire("finance", ResourceGoroutines)
	m.Acquire("finance", ResourceGoroutines)

	snap := m.Snapshot("finance")
	entry, ok := snap[ResourceGoroutines]
	if !ok {
		t.Fatal("expected goroutines in snapshot")
	}
	if entry.Current != 2 || entry.Limit != 10 {
		t.Fatalf("expected current=2 limit=10, got current=%d limit=%d", entry.Current, entry.Limit)
	}
}

func TestManagerConcurrentAccess(t *testing.T) {
	m := NewManager()
	m.SetLimit("tenant", ResourceGoroutines, 1000)

	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	// 100 goroutines each acquiring and releasing 10 times
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if err := m.Acquire("tenant", ResourceGoroutines); err != nil {
					mu.Lock()
					errCount++
					mu.Unlock()
					return
				}
				m.Release("tenant", ResourceGoroutines)
			}
		}()
	}

	wg.Wait()

	if errCount > 0 {
		t.Fatalf("unexpected errors during concurrent access: %d", errCount)
	}

	// All acquired resources should be released
	if m.Usage("tenant", ResourceGoroutines) != 0 {
		t.Fatalf("expected usage 0 after all releases, got %d", m.Usage("tenant", ResourceGoroutines))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/resource/ -v -count=1
```
Expected: FAIL — `NewManager`, `SetLimit`, etc. not defined (stub only has empty struct)

- [ ] **Step 3: Implement resource manager**

Replace `kernel/resource/manager.go`:
```go
package resource

import (
	"errors"
	"sync"
)

// ResourceType identifies a trackable resource.
type ResourceType int

const (
	ResourceGoroutines      ResourceType = iota
	ResourceFileDescriptors
	ResourceAPICalls
)

// ErrLimitExceeded is returned when acquiring a resource would exceed the limit.
var ErrLimitExceeded = errors.New("resource: limit exceeded")

// UsageEntry represents current usage and limit for a resource.
type UsageEntry struct {
	Current int64
	Limit   int64 // 0 means unlimited
}

// Manager tracks resource usage per tenant and enforces limits.
type Manager struct {
	mu     sync.RWMutex
	usage  map[string]map[ResourceType]int64
	limits map[string]map[ResourceType]int64
}

// NewManager creates a new resource manager.
func NewManager() *Manager {
	return &Manager{
		usage:  make(map[string]map[ResourceType]int64),
		limits: make(map[string]map[ResourceType]int64),
	}
}

// SetLimit sets the maximum allowed usage for a resource for a tenant.
func (m *Manager) SetLimit(tenant string, res ResourceType, limit int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.limits[tenant] == nil {
		m.limits[tenant] = make(map[ResourceType]int64)
	}
	m.limits[tenant][res] = limit
}

// Acquire increments the counter. Returns ErrLimitExceeded if over limit.
func (m *Manager) Acquire(tenant string, res ResourceType) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.usage[tenant] == nil {
		m.usage[tenant] = make(map[ResourceType]int64)
	}

	current := m.usage[tenant][res]
	limit := m.getLimit(tenant, res)

	if limit > 0 && current >= limit {
		return ErrLimitExceeded
	}

	m.usage[tenant][res] = current + 1
	return nil
}

// Release decrements the counter for a tenant.
func (m *Manager) Release(tenant string, res ResourceType) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.usage[tenant] != nil && m.usage[tenant][res] > 0 {
		m.usage[tenant][res]--
	}
}

// Usage returns the current usage of a resource for a tenant.
func (m *Manager) Usage(tenant string, res ResourceType) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.usage[tenant] == nil {
		return 0
	}
	return m.usage[tenant][res]
}

// Snapshot returns all tracked resources and their usage for a tenant.
func (m *Manager) Snapshot(tenant string) map[ResourceType]UsageEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[ResourceType]UsageEntry)

	if limits, ok := m.limits[tenant]; ok {
		for res, limit := range limits {
			var current int64
			if m.usage[tenant] != nil {
				current = m.usage[tenant][res]
			}
			result[res] = UsageEntry{Current: current, Limit: limit}
		}
	}

	if usage, ok := m.usage[tenant]; ok {
		for res, current := range usage {
			if _, exists := result[res]; !exists {
				result[res] = UsageEntry{Current: current, Limit: 0}
			}
		}
	}

	return result
}

func (m *Manager) getLimit(tenant string, res ResourceType) int64 {
	if m.limits[tenant] == nil {
		return 0
	}
	return m.limits[tenant][res]
}
```

- [ ] **Step 4: Run tests with race detector**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/resource/ -v -count=1 -race
```
Expected: All tests PASS, no race conditions

- [ ] **Step 5: Commit**

```bash
git add kernel/resource/manager.go kernel/resource/manager_test.go
git commit -m "feat: implement per-tenant resource manager with concurrent access tests"
```

---

### Task 9: Update CLI with Start Command and SIGHUP Hot Reload

**Files:**
- Modify: `cmd/cyntr/main.go`
- Create: `cyntr.yaml` (default config)

- [ ] **Step 1: Create default config file**

Create `cyntr.yaml` at project root:
```yaml
version: "1"
listen:
  address: "127.0.0.1:8080"
  webui: ":7700"
tenants: {}
```

- [ ] **Step 2: Update main.go with start command, SIGHUP handling**

Replace `cmd/cyntr/main.go`:
```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cyntr-dev/cyntr/kernel"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: cyntr <command>")
		fmt.Fprintln(os.Stderr, "commands: start, version")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "version":
		fmt.Printf("cyntr v%s\n", version)
	case "start":
		runStart()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func runStart() {
	cfgPath := "cyntr.yaml"
	if len(os.Args) > 2 {
		cfgPath = os.Args[2]
	}

	k := kernel.New()

	if err := k.LoadConfig(cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := k.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "start error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("cyntr started")

	// Signal handling: SIGINT/SIGTERM for shutdown, SIGHUP for config reload
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for sig := range sigCh {
		switch sig {
		case syscall.SIGHUP:
			fmt.Println("received SIGHUP, reloading config...")
			if err := k.ReloadConfig(); err != nil {
				fmt.Fprintf(os.Stderr, "config reload error: %v\n", err)
			} else {
				fmt.Println("config reloaded")
			}
		case syscall.SIGINT, syscall.SIGTERM:
			fmt.Printf("\nreceived %s, shutting down...\n", sig)
			if err := k.Stop(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "stop error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("cyntr stopped")
			return
		}
	}
}
```

- [ ] **Step 3: Add ReloadConfig to kernel**

Add to `kernel/kernel.go`:
```go
// ReloadConfig re-reads the config file and notifies listeners.
func (k *Kernel) ReloadConfig() error {
	if k.services.Config == nil {
		return fmt.Errorf("no config loaded")
	}
	return k.services.Config.Reload()
}
```

- [ ] **Step 4: Build and verify**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go build -o cyntr ./cmd/cyntr && ./cyntr version
```
Expected: `cyntr v0.1.0`

- [ ] **Step 5: Test start command**

Run:
```bash
cd /Users/suryakoritala/Cyntr && timeout 2 ./cyntr start cyntr.yaml 2>&1 || true
```
Expected: `cyntr started` printed

- [ ] **Step 6: Commit**

```bash
git add cmd/cyntr/main.go cyntr.yaml kernel/kernel.go
git commit -m "feat: add CLI start command with SIGHUP hot config reload and signal handling"
```

---

### Task 10: Kernel Integration Test — Modules Using IPC Bus

**Files:**
- Modify: `kernel/kernel_test.go`

- [ ] **Step 1: Write integration test with modules communicating via IPC bus**

Add to `kernel/kernel_test.go`:
```go
// ipcTestModule is a module that uses the IPC bus to send and receive messages.
type ipcTestModule struct {
	name     string
	deps     []string
	healthy  bool
	bus      *ipc.Bus
	received chan string
}

func (m *ipcTestModule) Name() string           { return m.name }
func (m *ipcTestModule) Dependencies() []string  { return m.deps }

func (m *ipcTestModule) Init(ctx context.Context, svc *Services) error {
	m.bus = svc.Bus
	return nil
}

func (m *ipcTestModule) Start(ctx context.Context) error {
	// Register a handler that echoes the payload back
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
```

Also add the import for `ipc`:
```go
import (
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)
```

Then add the integration test:
```go
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

	// Caller sends a request to responder via the shared IPC bus
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

	// Verify responder actually received it
	select {
	case val := <-received:
		if val != "hello" {
			t.Fatalf("responder received %q, expected 'hello'", val)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for responder to receive message")
	}
}
```

- [ ] **Step 2: Run integration test**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./kernel/ -run TestKernelIntegration -v -count=1 -race
```
Expected: PASS — two modules communicating via the real IPC bus

- [ ] **Step 3: Run full test suite**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./... -v -count=1 -race
```
Expected: All tests PASS across all packages, no race conditions

- [ ] **Step 4: Run go vet**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go vet ./...
```
Expected: No issues

- [ ] **Step 5: Commit**

```bash
git add kernel/kernel_test.go
git commit -m "feat: add kernel integration test with real IPC bus communication between modules"
```

---

### Task 11: Final Verification

- [ ] **Step 1: Run complete test suite with race detector**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go test ./... -v -count=1 -race
```
Expected: All tests PASS, no race conditions

- [ ] **Step 2: Run go vet**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go vet ./...
```
Expected: No issues

- [ ] **Step 3: Build final binary**

Run:
```bash
cd /Users/suryakoritala/Cyntr && go build -o cyntr ./cmd/cyntr && ./cyntr version
```
Expected: `cyntr v0.1.0`

- [ ] **Step 4: Verify project structure**

Run:
```bash
cd /Users/suryakoritala/Cyntr && find . -name '*.go' | sort
```
Expected:
```
./cmd/cyntr/main.go
./kernel/config/schema.go
./kernel/config/store.go
./kernel/config/store_test.go
./kernel/ipc/bus.go
./kernel/ipc/bus_test.go
./kernel/ipc/types.go
./kernel/ipc/types_test.go
./kernel/kernel.go
./kernel/kernel_test.go
./kernel/module.go
./kernel/module_test.go
./kernel/resource/manager.go
./kernel/resource/manager_test.go
```

- [ ] **Step 5: Verify clean git status**

Run:
```bash
git status
```
Expected: Clean working tree
