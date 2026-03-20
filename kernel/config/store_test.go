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
		t.Fatalf("expected 'process', got '%s'", finance.Isolation)
	}
	if finance.Cgroup.MemoryLimit != "2GB" {
		t.Fatalf("expected '2GB', got '%s'", finance.Cgroup.MemoryLimit)
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
		t.Fatalf("expected 1, got %d", notified.Load())
	}

	cfg := store.Get()
	if cfg.Version != "2" {
		t.Fatalf("expected '2', got '%s'", cfg.Version)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Version != "1" {
		t.Fatalf("expected '1', got '%s'", cfg.Version)
	}
	if cfg.Listen.Address != "127.0.0.1:8080" {
		t.Fatalf("expected default address, got '%s'", cfg.Listen.Address)
	}
}
