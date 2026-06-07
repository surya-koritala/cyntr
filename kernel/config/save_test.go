package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSaveAndTenantPersistence verifies that runtime tenant mutations are
// written back to the backing file and survive a reload (the dashboard
// tenant-create bug: created tenants must persist, not live only in memory).
func TestSaveAndTenantPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cyntr.yaml")
	if err := os.WriteFile(path, []byte("version: \"1\"\nlisten:\n  address: \"127.0.0.1:8080\"\ntenants:\n  acme:\n    isolation: namespace\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Add a tenant and persist.
	if err := store.SetTenant("globex", TenantConfig{Isolation: "process", Policy: "strict"}); err != nil {
		t.Fatalf("set tenant: %v", err)
	}

	// A fresh load from disk must see both tenants.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got := reloaded.Get().Tenants
	if len(got) != 2 || got["acme"].Isolation != "namespace" || got["globex"].Isolation != "process" {
		t.Fatalf("expected persisted acme+globex, got %+v", got)
	}

	// Removing a tenant persists too.
	if err := store.RemoveTenant("acme"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	reloaded2, _ := Load(path)
	if _, ok := reloaded2.Get().Tenants["acme"]; ok {
		t.Fatalf("acme should have been removed from disk")
	}
	if _, ok := reloaded2.Get().Tenants["globex"]; !ok {
		t.Fatalf("globex should remain")
	}

	// Existing file mode is preserved (0644), not widened or reset.
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("expected mode 0644 preserved, got %o", fi.Mode().Perm())
	}
}
