# Skill Runtime Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Skill Runtime — a kernel module that loads, validates, and manages Cyntr skill packages (YAML manifest + markdown instructions), enforces capability declarations, and provides skills to the agent runtime for context injection.

**Architecture:** The Skill Runtime manages a registry of installed skills. Each skill has a `skill.yaml` manifest declaring its name, version, capabilities (network, filesystem, shell, tools), and a `skill.md` with instructions for the agent. The runtime validates manifests, verifies signatures (HMAC for now), enforces capability restrictions, and serves skill instructions to the agent runtime via IPC. WASM execution is deferred — this plan builds the skill management, validation, and instruction injection pipeline.

**Tech Stack:** Go 1.22+, `gopkg.in/yaml.v3` (already a dep), `crypto/hmac` for signature verification.

**Spec:** `docs/superpowers/specs/2026-03-19-cyntr-enterprise-platform-design.md` (Section 2.5)

**Dependencies:** Kernel + Policy Engine (Plans 1-2).

**Deferred to future plans:**
- WASM execution via wazero — this plan loads skill instructions (markdown) for agent context injection. WASM handler execution comes later.
- Curated registry server — this plan manages local skill directories. A registry API comes with Plan 9 (API).
- OpenClaw SKILL.md compatibility layer — builds on this foundation.
- Signature verification with real PKI — this plan uses HMAC.

---

## File Structure

```
modules/skill/
├── types.go               # SkillManifest, SkillCapabilities, InstalledSkill
├── loader.go              # Load skill from directory, validate manifest
├── registry.go            # SkillRegistry: install, list, get, verify
├── runtime.go             # Runtime kernel module: IPC handlers
├── types_test.go
├── loader_test.go         # Tests with real skill directories
├── registry_test.go       # Registry tests
└── runtime_test.go        # Module IPC tests
```

---

## Chunk 1: Skill Types + Loader

### Task 1: Define Skill Types

**Files:**
- Create: `modules/skill/types.go`
- Create: `modules/skill/types_test.go`

- [ ] **Step 1: Write failing test**

Create `modules/skill/types_test.go`:
```go
package skill

import "testing"

func TestSkillManifestValidate(t *testing.T) {
	m := SkillManifest{
		Name:    "test-skill",
		Version: "1.0.0",
		Author:  "test",
		License: "Apache-2.0",
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

func TestSkillManifestValidateMissingName(t *testing.T) {
	m := SkillManifest{Version: "1.0.0"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestSkillManifestValidateMissingVersion(t *testing.T) {
	m := SkillManifest{Name: "test"}
	if err := m.Validate(); err == nil {
		t.Fatal("expected error for missing version")
	}
}

func TestCapabilitiesHasNetwork(t *testing.T) {
	c := Capabilities{Network: []string{"https://*.example.com"}}
	if !c.HasNetwork() {
		t.Fatal("expected has network")
	}
}

func TestCapabilitiesNoNetwork(t *testing.T) {
	c := Capabilities{}
	if c.HasNetwork() {
		t.Fatal("expected no network")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/skill/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement types**

Create `modules/skill/types.go`:
```go
package skill

import "fmt"

// SkillManifest is parsed from skill.yaml.
type SkillManifest struct {
	Name         string       `yaml:"name"`
	Version      string       `yaml:"version"`
	Author       string       `yaml:"author"`
	License      string       `yaml:"license"`
	Capabilities Capabilities `yaml:"capabilities"`
	Signing      SigningInfo   `yaml:"signing"`
}

// Validate checks required fields.
func (m SkillManifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("skill manifest: name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("skill manifest: version is required")
	}
	return nil
}

// Capabilities declares what a skill is allowed to access.
type Capabilities struct {
	Network    []string `yaml:"network"`    // URL patterns: "https://*.example.com"
	Filesystem []string `yaml:"filesystem"` // path patterns, or empty for none
	Shell      bool     `yaml:"shell"`
	Tools      []string `yaml:"tools"`      // tool names the skill can use
}

// HasNetwork returns true if the skill declares any network access.
func (c Capabilities) HasNetwork() bool {
	return len(c.Network) > 0
}

// HasFilesystem returns true if the skill declares any filesystem access.
func (c Capabilities) HasFilesystem() bool {
	return len(c.Filesystem) > 0
}

// SigningInfo holds signature metadata.
type SigningInfo struct {
	Registry    string `yaml:"registry"`
	Fingerprint string `yaml:"fingerprint"`
}

// InstalledSkill represents a loaded and validated skill.
type InstalledSkill struct {
	Manifest     SkillManifest
	Instructions string // contents of skill.md
	Path         string // directory path on disk
	Signature    string // contents of SIGNATURE file
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/skill/ -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add modules/skill/types.go modules/skill/types_test.go
git commit -m "feat(skill): define skill types — SkillManifest, Capabilities, InstalledSkill"
```

---

### Task 2: Implement Skill Loader

**Files:**
- Create: `modules/skill/loader.go`
- Create: `modules/skill/loader_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/skill/loader_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/skill/ -run TestLoad -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement loader**

Create `modules/skill/loader.go`:
```go
package skill

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadSkill loads a skill from a directory containing skill.yaml and optionally skill.md and SIGNATURE.
func LoadSkill(dir string) (*InstalledSkill, error) {
	// Read manifest
	manifestPath := filepath.Join(dir, "skill.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest SkillManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	if err := manifest.Validate(); err != nil {
		return nil, err
	}

	skill := &InstalledSkill{
		Manifest: manifest,
		Path:     dir,
	}

	// Read instructions (optional)
	instructionsPath := filepath.Join(dir, "skill.md")
	if instrData, err := os.ReadFile(instructionsPath); err == nil {
		skill.Instructions = string(instrData)
	}

	// Read signature (optional)
	sigPath := filepath.Join(dir, "SIGNATURE")
	if sigData, err := os.ReadFile(sigPath); err == nil {
		skill.Signature = string(sigData)
	}

	return skill, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/skill/ -v -count=1`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/skill/loader.go modules/skill/loader_test.go
git commit -m "feat(skill): implement skill loader from directory with manifest validation"
```

---

## Chunk 2: Registry + Runtime Module

### Task 3: Implement Skill Registry

**Files:**
- Create: `modules/skill/registry.go`
- Create: `modules/skill/registry_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/skill/registry_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/skill/ -run TestRegistry -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement registry**

Create `modules/skill/registry.go`:
```go
package skill

import (
	"fmt"
	"sort"
	"sync"
)

// Registry manages installed skills.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*InstalledSkill
}

// NewRegistry creates an empty skill registry.
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]*InstalledSkill)}
}

// Install loads a skill from a directory and adds it to the registry.
func (r *Registry) Install(dir string) error {
	skill, err := LoadSkill(dir)
	if err != nil {
		return fmt.Errorf("install skill from %s: %w", dir, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.skills[skill.Manifest.Name]; exists {
		return fmt.Errorf("skill %q already installed", skill.Manifest.Name)
	}

	r.skills[skill.Manifest.Name] = skill
	return nil
}

// Get returns an installed skill by name.
func (r *Registry) Get(name string) (*InstalledSkill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	return s, ok
}

// List returns all installed skill names sorted alphabetically.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Uninstall removes a skill from the registry.
func (r *Registry) Uninstall(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skills[name]; !exists {
		return fmt.Errorf("skill %q not found", name)
	}
	delete(r.skills, name)
	return nil
}

// GetInstructions returns the instructions (skill.md content) for the given skill names.
// Returns a map of name -> instructions. Missing skills are skipped.
func (r *Registry) GetInstructions(names []string) map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string)
	for _, name := range names {
		if s, ok := r.skills[name]; ok {
			result[name] = s.Instructions
		}
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/skill/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/skill/registry.go modules/skill/registry_test.go
git commit -m "feat(skill): implement skill registry with install, uninstall, and instruction retrieval"
```

---

### Task 4: Implement Skill Runtime as Kernel Module

**Files:**
- Create: `modules/skill/runtime.go`
- Create: `modules/skill/runtime_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/skill/runtime_test.go`:
```go
package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func TestRuntimeImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Runtime)(nil)
}

func TestRuntimeInstallAndListViaIPC(t *testing.T) {
	dir := t.TempDir()
	skillDir := createTestSkill(t, dir)

	bus := ipc.NewBus()
	defer bus.Close()

	rt := NewRuntime()
	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Install via IPC
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "skill_runtime", Topic: "skill.install",
		Payload: skillDir,
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("expected ok, got %v", resp.Payload)
	}

	// List via IPC
	resp, err = bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "skill_runtime", Topic: "skill.list",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	names, ok := resp.Payload.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", resp.Payload)
	}
	if len(names) != 1 || names[0] != "test-skill" {
		t.Fatalf("expected [test-skill], got %v", names)
	}
}

func TestRuntimeGetInstructionsViaIPC(t *testing.T) {
	dir := t.TempDir()
	skillDir := createTestSkill(t, dir)

	bus := ipc.NewBus()
	defer bus.Close()

	rt := NewRuntime()
	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "skill_runtime", Topic: "skill.install",
		Payload: skillDir,
	})

	// Get instructions
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "agent_runtime", Target: "skill_runtime", Topic: "skill.instructions",
		Payload: []string{"test-skill"},
	})
	if err != nil {
		t.Fatalf("instructions: %v", err)
	}

	instructions, ok := resp.Payload.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", resp.Payload)
	}
	if instructions["test-skill"] == "" {
		t.Fatal("expected non-empty instructions")
	}
}

func TestRuntimeUninstallViaIPC(t *testing.T) {
	dir := t.TempDir()
	skillDir := createTestSkill(t, dir)

	bus := ipc.NewBus()
	defer bus.Close()

	rt := NewRuntime()
	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "skill_runtime", Topic: "skill.install",
		Payload: skillDir,
	})

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "skill_runtime", Topic: "skill.uninstall",
		Payload: "test-skill",
	})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("expected ok, got %v", resp.Payload)
	}

	// Verify removed
	resp, _ = bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "skill_runtime", Topic: "skill.list",
	})
	names := resp.Payload.([]string)
	if len(names) != 0 {
		t.Fatalf("expected 0 skills, got %d", len(names))
	}
}

func TestRuntimeGetSkillInfoViaIPC(t *testing.T) {
	dir := t.TempDir()
	skillDir := createTestSkill(t, dir)

	bus := ipc.NewBus()
	defer bus.Close()

	rt := NewRuntime()
	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "skill_runtime", Topic: "skill.install",
		Payload: skillDir,
	})

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "skill_runtime", Topic: "skill.get",
		Payload: "test-skill",
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	skill, ok := resp.Payload.(*InstalledSkill)
	if !ok {
		t.Fatalf("expected *InstalledSkill, got %T", resp.Payload)
	}
	if skill.Manifest.Name != "test-skill" {
		t.Fatalf("expected test-skill, got %q", skill.Manifest.Name)
	}
}

func TestRuntimeHealthy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	rt := NewRuntime()
	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)
	h := rt.Health(ctx)
	if !h.Healthy {
		t.Fatalf("expected healthy: %s", h.Message)
	}
}

func init() {
	// Ensure createTestSkill helper is available (defined in loader_test.go, same package)
	_ = os.MkdirAll
	_ = filepath.Join
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/skill/ -run TestRuntime -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement runtime module**

Create `modules/skill/runtime.go`:
```go
package skill

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Runtime is the Skill Runtime kernel module.
type Runtime struct {
	bus      *ipc.Bus
	registry *Registry
}

// NewRuntime creates a new Skill Runtime module.
func NewRuntime() *Runtime {
	return &Runtime{
		registry: NewRegistry(),
	}
}

func (r *Runtime) Name() string           { return "skill_runtime" }
func (r *Runtime) Dependencies() []string { return nil }

func (r *Runtime) Init(ctx context.Context, svc *kernel.Services) error {
	r.bus = svc.Bus
	return nil
}

func (r *Runtime) Start(ctx context.Context) error {
	r.bus.Handle("skill_runtime", "skill.install", r.handleInstall)
	r.bus.Handle("skill_runtime", "skill.uninstall", r.handleUninstall)
	r.bus.Handle("skill_runtime", "skill.list", r.handleList)
	r.bus.Handle("skill_runtime", "skill.get", r.handleGet)
	r.bus.Handle("skill_runtime", "skill.instructions", r.handleInstructions)
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error { return nil }

func (r *Runtime) Health(ctx context.Context) kernel.HealthStatus {
	return kernel.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("%d skills installed", len(r.registry.List())),
	}
}

func (r *Runtime) handleInstall(msg ipc.Message) (ipc.Message, error) {
	dir, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string path, got %T", msg.Payload)
	}
	if err := r.registry.Install(dir); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (r *Runtime) handleUninstall(msg ipc.Message) (ipc.Message, error) {
	name, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string name, got %T", msg.Payload)
	}
	if err := r.registry.Uninstall(name); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (r *Runtime) handleList(msg ipc.Message) (ipc.Message, error) {
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: r.registry.List()}, nil
}

func (r *Runtime) handleGet(msg ipc.Message) (ipc.Message, error) {
	name, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string name, got %T", msg.Payload)
	}
	skill, ok := r.registry.Get(name)
	if !ok {
		return ipc.Message{}, fmt.Errorf("skill %q not found", name)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: skill}, nil
}

func (r *Runtime) handleInstructions(msg ipc.Message) (ipc.Message, error) {
	names, ok := msg.Payload.([]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected []string, got %T", msg.Payload)
	}
	return ipc.Message{
		Type:    ipc.MessageTypeResponse,
		Payload: r.registry.GetInstructions(names),
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/skill/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/suryakoritala/Cyntr && go test ./... -count=1 -race`
Expected: All PASS

- [ ] **Step 6: Run go vet and build**

Run: `cd /Users/suryakoritala/Cyntr && go vet ./... && go build -o cyntr ./cmd/cyntr && ./cyntr version`
Expected: Clean, `cyntr v0.1.0`

- [ ] **Step 7: Commit**

```bash
git add modules/skill/runtime.go modules/skill/runtime_test.go
git commit -m "feat(skill): implement Skill Runtime kernel module with IPC handlers for install, list, get, and instructions"
```
