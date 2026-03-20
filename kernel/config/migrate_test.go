package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigratorNeedsMigration(t *testing.T) {
	m := NewMigrator()
	cfg := CyntrConfig{Version: "1"}
	if !m.NeedsMigration(cfg, "2") {
		t.Fatal("expected needs migration")
	}
	cfg.Version = "2"
	if m.NeedsMigration(cfg, "2") {
		t.Fatal("should not need migration")
	}
}

func TestMigratorV1toV2(t *testing.T) {
	m := NewMigrator()
	cfg := CyntrConfig{Version: "1"}

	if err := m.Migrate(&cfg, "2"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if cfg.Version != "2" {
		t.Fatalf("expected version 2, got %q", cfg.Version)
	}
	if cfg.Audit.StoragePath != "~/.cyntr/audit" {
		t.Fatalf("expected audit path, got %q", cfg.Audit.StoragePath)
	}
	if cfg.Audit.Retention != "365d" {
		t.Fatalf("expected retention, got %q", cfg.Audit.Retention)
	}
}

func TestMigratorPreservesExisting(t *testing.T) {
	m := NewMigrator()
	cfg := CyntrConfig{
		Version: "1",
		Audit:   AuditConfig{StoragePath: "/custom/path", Retention: "30d"},
	}

	m.Migrate(&cfg, "2")

	if cfg.Audit.StoragePath != "/custom/path" {
		t.Fatalf("should preserve existing: got %q", cfg.Audit.StoragePath)
	}
}

func TestMigratorAlreadyAtTarget(t *testing.T) {
	m := NewMigrator()
	cfg := CyntrConfig{Version: "2"}

	if err := m.Migrate(&cfg, "2"); err != nil {
		t.Fatalf("should succeed: %v", err)
	}
}

func TestMigratorFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cyntr.yaml")
	os.WriteFile(path, []byte("version: \"1\"\nlisten:\n  address: \"127.0.0.1:8080\"\n"), 0644)

	m := NewMigrator()
	if err := m.MigrateFile(path, "2"); err != nil {
		t.Fatalf("migrate file: %v", err)
	}

	store, _ := Load(path)
	cfg := store.Get()
	if cfg.Version != "2" {
		t.Fatalf("expected 2, got %q", cfg.Version)
	}
}

func TestMigratorNoPath(t *testing.T) {
	m := NewMigrator()
	cfg := CyntrConfig{Version: "99"}
	if err := m.Migrate(&cfg, "100"); err == nil {
		t.Fatal("expected error for no migration path")
	}
}
