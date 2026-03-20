package kernel

import (
	"context"
	"fmt"
	"sync"

	"github.com/cyntr-dev/cyntr/kernel/config"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/resource"
)

type Kernel struct {
	mu           sync.RWMutex
	modules      []moduleEntry
	moduleIndex  map[string]int
	services     *Services
	running      bool
	configLoaded bool
	bootOrder    []int
}

type moduleEntry struct {
	module Module
	state  ModuleState
}

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

func (k *Kernel) LoadConfig(path string) error {
	store, err := config.Load(path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	k.services.Config = store
	k.configLoaded = true
	return nil
}

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

func (k *Kernel) Modules() []string {
	k.mu.RLock()
	defer k.mu.RUnlock()

	names := make([]string, len(k.modules))
	for i, entry := range k.modules {
		names[i] = entry.module.Name()
	}
	return names
}

func (k *Kernel) ModuleState(name string) ModuleState {
	k.mu.RLock()
	defer k.mu.RUnlock()

	if idx, ok := k.moduleIndex[name]; ok {
		return k.modules[idx].state
	}
	return ModuleStateFailed
}

func (k *Kernel) Start(ctx context.Context) error {
	k.mu.Lock()

	if !k.configLoaded {
		k.mu.Unlock()
		return fmt.Errorf("config not loaded: call LoadConfig before Start")
	}

	order, err := k.topoSort()
	if err != nil {
		k.mu.Unlock()
		return err
	}
	k.bootOrder = order

	entries := make([]int, len(order))
	copy(entries, order)
	services := k.services
	k.mu.Unlock()

	// Phase 1: Initialize
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

	// Phase 2: Start
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

func (k *Kernel) Stop(ctx context.Context) error {
	k.mu.Lock()
	order := make([]int, len(k.bootOrder))
	copy(order, k.bootOrder)
	k.mu.Unlock()

	var firstErr error

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

func (k *Kernel) HealthReport(ctx context.Context) map[string]HealthStatus {
	k.mu.RLock()
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

// Bus returns the IPC bus for integration testing and CLI use.
func (k *Kernel) Bus() *ipc.Bus {
	return k.services.Bus
}

func (k *Kernel) Config() *config.Store {
	return k.services.Config
}

func (k *Kernel) ResourceManager() *resource.Manager {
	return k.services.Resources
}

// ReloadConfig re-reads the config file and notifies listeners.
func (k *Kernel) ReloadConfig() error {
	if k.services.Config == nil {
		return fmt.Errorf("no config loaded")
	}
	return k.services.Config.Reload()
}

func (k *Kernel) topoSort() ([]int, error) {
	n := len(k.modules)
	inDegree := make([]int, n)
	adj := make([][]int, n)

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
