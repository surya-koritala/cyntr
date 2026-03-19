package tenant

import (
	"fmt"
	"sort"
	"sync"
	"github.com/cyntr-dev/cyntr/kernel/config"
	"github.com/cyntr-dev/cyntr/kernel/resource"
)

type IsolationMode int

const (
	IsolationNamespace IsolationMode = iota
	IsolationProcess
)

func (m IsolationMode) String() string {
	switch m {
	case IsolationNamespace: return "namespace"
	case IsolationProcess: return "process"
	default: return fmt.Sprintf("unknown(%d)", int(m))
	}
}

type Tenant struct {
	Name      string
	Isolation IsolationMode
	Policy    string
}

type Manager struct {
	mu        sync.RWMutex
	tenants   map[string]*Tenant
	resources *resource.Manager
}

func NewManager(cfg config.CyntrConfig, rm *resource.Manager) (*Manager, error) {
	tm := &Manager{tenants: make(map[string]*Tenant), resources: rm}
	for name, tc := range cfg.Tenants {
		mode, err := parseIsolation(tc.Isolation)
		if err != nil { return nil, fmt.Errorf("tenant %q: %w", name, err) }
		tm.tenants[name] = &Tenant{Name: name, Isolation: mode, Policy: tc.Policy}
	}
	return tm, nil
}

func (m *Manager) Get(name string) (Tenant, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tenants[name]
	if !ok { return Tenant{}, false }
	return *t, true
}

func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.tenants))
	for name := range m.tenants { names = append(names, name) }
	sort.Strings(names)
	return names
}

func (m *Manager) Create(name string, isolation IsolationMode, policy string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tenants[name]; exists { return fmt.Errorf("tenant %q already exists", name) }
	m.tenants[name] = &Tenant{Name: name, Isolation: isolation, Policy: policy}
	return nil
}

func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.tenants[name]; !exists { return fmt.Errorf("tenant %q not found", name) }
	delete(m.tenants, name)
	return nil
}

func parseIsolation(s string) (IsolationMode, error) {
	switch s {
	case "namespace", "": return IsolationNamespace, nil
	case "process": return IsolationProcess, nil
	default: return IsolationNamespace, fmt.Errorf("invalid isolation mode %q", s)
	}
}
