package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func createTestSkill(t *testing.T, dir string) string {
	t.Helper()
	skillDir := filepath.Join(dir, "test-skill")
	os.MkdirAll(skillDir, 0755)

	os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte(`
name: test-skill
version: 1.0.0
author: test-author
license: Apache-2.0
capabilities:
  network:
    - "https://*.example.com"
  shell: false
  tools:
    - http_request
signing:
  registry: test.registry
  fingerprint: "sha256:abc123"
`), 0644)

	os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte(`# Test Skill

You are a test skill. Use the http_request tool to fetch data from example.com.
`), 0644)

	os.WriteFile(filepath.Join(skillDir, "SIGNATURE"), []byte("test-signature"), 0644)

	return skillDir
}

func TestLoadSkill(t *testing.T) {
	dir := t.TempDir()
	skillDir := createTestSkill(t, dir)

	skill, err := LoadSkill(skillDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if skill.Manifest.Name != "test-skill" {
		t.Fatalf("expected test-skill, got %q", skill.Manifest.Name)
	}
	if skill.Manifest.Version != "1.0.0" {
		t.Fatalf("expected 1.0.0, got %q", skill.Manifest.Version)
	}
	if skill.Manifest.Author != "test-author" {
		t.Fatalf("expected test-author, got %q", skill.Manifest.Author)
	}
	if !skill.Manifest.Capabilities.HasNetwork() {
		t.Fatal("expected network capability")
	}
	if len(skill.Manifest.Capabilities.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(skill.Manifest.Capabilities.Tools))
	}
	if skill.Instructions == "" {
		t.Fatal("expected instructions")
	}
	if skill.Signature != "test-signature" {
		t.Fatalf("expected signature, got %q", skill.Signature)
	}
}

func TestLoadSkillMissingManifest(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadSkill(dir)
	if err == nil {
		t.Fatal("expected error for missing manifest")
	}
}

func TestLoadSkillInvalidManifest(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("{{{invalid"), 0644)
	_, err := LoadSkill(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestLoadSkillMissingName(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("version: 1.0.0\n"), 0644)
	_, err := LoadSkill(dir)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestLoadSkillNoInstructions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte("name: test\nversion: 1.0.0\n"), 0644)
	// No skill.md — should still load, instructions will be empty
	skill, err := LoadSkill(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if skill.Instructions != "" {
		t.Fatalf("expected empty instructions, got %q", skill.Instructions)
	}
}
