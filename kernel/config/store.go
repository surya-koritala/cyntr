package config

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type Store struct {
	mu        sync.RWMutex
	path      string
	config    CyntrConfig
	listeners []func(CyntrConfig)
}

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

func Load(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg CyntrConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	return &Store{path: path, config: cfg}, nil
}

func (s *Store) Get() CyntrConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *Store) OnChange(fn func(CyntrConfig)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

// Save writes the current in-memory config back to its backing file and
// notifies listeners. Runtime mutations (e.g. creating a tenant from the
// dashboard) call this so the change survives a restart and is reflected by
// readers that consult the config. The file's existing permissions are
// preserved (defaulting to 0600 for a new file) so secrets/identifiers aren't
// widened.
func (s *Store) Save() error {
	s.mu.RLock()
	cfg := s.config
	path := s.path
	listeners := make([]func(CyntrConfig), len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.RUnlock()

	if path == "" {
		return fmt.Errorf("config has no backing file")
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	mode := os.FileMode(0o600)
	if fi, statErr := os.Stat(path); statErr == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	for _, fn := range listeners {
		fn(cfg)
	}
	return nil
}

// SetTenant adds or updates a tenant and persists the config to disk.
func (s *Store) SetTenant(name string, tc TenantConfig) error {
	s.mu.Lock()
	if s.config.Tenants == nil {
		s.config.Tenants = make(map[string]TenantConfig)
	}
	s.config.Tenants[name] = tc
	s.mu.Unlock()
	return s.Save()
}

// RemoveTenant deletes a tenant and persists the config to disk.
func (s *Store) RemoveTenant(name string) error {
	s.mu.Lock()
	delete(s.config.Tenants, name)
	s.mu.Unlock()
	return s.Save()
}

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
