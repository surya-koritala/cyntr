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
