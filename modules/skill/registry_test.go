package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryInstallAndGet(t *testing.T) {
	dir := t.TempDir()
	skillDir := createTestSkill(t, dir)

	reg := NewRegistry()
	if err := reg.Install(skillDir); err != nil {
		t.Fatalf("install: %v", err)
	}

	skill, ok := reg.Get("test-skill")
	if !ok {
		t.Fatal("expected to find test-skill")
	}
	if skill.Manifest.Name != "test-skill" {
		t.Fatalf("expected test-skill, got %q", skill.Manifest.Name)
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	reg := NewRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestRegistryList(t *testing.T) {
	dir := t.TempDir()

	// Create two skills
	skill1 := filepath.Join(dir, "skill-a")
	os.MkdirAll(skill1, 0755)
	os.WriteFile(filepath.Join(skill1, "skill.yaml"), []byte("name: alpha\nversion: 1.0.0\n"), 0644)

	skill2 := filepath.Join(dir, "skill-b")
	os.MkdirAll(skill2, 0755)
	os.WriteFile(filepath.Join(skill2, "skill.yaml"), []byte("name: beta\nversion: 2.0.0\n"), 0644)

	reg := NewRegistry()
	reg.Install(skill1)
	reg.Install(skill2)

	names := reg.List()
	if len(names) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(names))
	}
	if names[0] != "alpha" {
		t.Fatalf("expected alpha first, got %q", names[0])
	}
}

func TestRegistryUninstall(t *testing.T) {
	dir := t.TempDir()
	skillDir := createTestSkill(t, dir)

	reg := NewRegistry()
	reg.Install(skillDir)

	if err := reg.Uninstall("test-skill"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	_, ok := reg.Get("test-skill")
	if ok {
		t.Fatal("expected skill to be removed")
	}
}

func TestRegistryUninstallNotFound(t *testing.T) {
	reg := NewRegistry()
	err := reg.Uninstall("nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRegistryInstallDuplicate(t *testing.T) {
	dir := t.TempDir()
	skillDir := createTestSkill(t, dir)

	reg := NewRegistry()
	reg.Install(skillDir)

	err := reg.Install(skillDir)
	if err == nil {
		t.Fatal("expected error for duplicate install")
	}
}

func TestRegistryGetInstructions(t *testing.T) {
	dir := t.TempDir()
	skillDir := createTestSkill(t, dir)

	reg := NewRegistry()
	reg.Install(skillDir)

	instructions := reg.GetInstructions([]string{"test-skill"})
	if len(instructions) != 1 {
		t.Fatalf("expected 1 instruction set, got %d", len(instructions))
	}
	if instructions["test-skill"] == "" {
		t.Fatal("expected non-empty instructions")
	}
}
