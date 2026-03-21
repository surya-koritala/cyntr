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
	found := false
	for _, n := range names {
		if n == "test-skill" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected test-skill in list, got %v", names)
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

	// Verify removed — test-skill should no longer appear (catalog skills remain)
	resp, _ = bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "skill_runtime", Topic: "skill.list",
	})
	names := resp.Payload.([]string)
	for _, n := range names {
		if n == "test-skill" {
			t.Fatalf("test-skill should have been uninstalled, but still in list: %v", names)
		}
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
