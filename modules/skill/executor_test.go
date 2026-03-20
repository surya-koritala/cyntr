package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func createTestSkillWithHandler(t *testing.T, dir, script string) *InstalledSkill {
	t.Helper()
	skillDir := filepath.Join(dir, "test-skill")
	handlersDir := filepath.Join(skillDir, "handlers")
	os.MkdirAll(handlersDir, 0755)

	os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: test\nversion: 1.0.0\n"), 0644)
	os.WriteFile(filepath.Join(handlersDir, "run.sh"), []byte(script), 0755)

	return &InstalledSkill{
		Manifest: SkillManifest{Name: "test", Version: "1.0.0"},
		Path:     skillDir,
	}
}

func TestScriptExecutorName(t *testing.T) {
	e := NewScriptExecutor(0)
	if e.Name() != "script" {
		t.Fatalf("expected script, got %q", e.Name())
	}
}

func TestScriptExecutorRunsScript(t *testing.T) {
	dir := t.TempDir()
	skill := createTestSkillWithHandler(t, dir, "#!/bin/sh\necho \"hello from skill\"")

	e := NewScriptExecutor(5 * time.Second)
	result, err := e.Execute(context.Background(), skill, "")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "hello from skill\n" {
		t.Fatalf("expected output, got %q", result.Output)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", result.ExitCode)
	}
}

func TestScriptExecutorPassesInput(t *testing.T) {
	dir := t.TempDir()
	skill := createTestSkillWithHandler(t, dir, "#!/bin/sh\ncat")

	e := NewScriptExecutor(5 * time.Second)
	result, err := e.Execute(context.Background(), skill, "input data")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "input data" {
		t.Fatalf("expected input echoed, got %q", result.Output)
	}
}

func TestScriptExecutorTimeout(t *testing.T) {
	dir := t.TempDir()
	skill := createTestSkillWithHandler(t, dir, "#!/bin/sh\nsleep 60")

	e := NewScriptExecutor(100 * time.Millisecond)
	result, err := e.Execute(context.Background(), skill, "")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Error == nil {
		t.Fatal("expected timeout error")
	}
}

func TestScriptExecutorNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	skill := createTestSkillWithHandler(t, dir, "#!/bin/sh\nexit 42")

	e := NewScriptExecutor(5 * time.Second)
	result, _ := e.Execute(context.Background(), skill, "")
	if result.ExitCode != 42 {
		t.Fatalf("expected exit 42, got %d", result.ExitCode)
	}
}

func TestScriptExecutorNilSkill(t *testing.T) {
	e := NewScriptExecutor(0)
	_, err := e.Execute(context.Background(), nil, "")
	if err == nil {
		t.Fatal("expected error for nil skill")
	}
}

func TestSandboxedExecutorName(t *testing.T) {
	inner := NewScriptExecutor(0)
	e := NewSandboxedExecutor(inner)
	if e.Name() != "sandboxed-script" {
		t.Fatalf("expected sandboxed-script, got %q", e.Name())
	}
}

func TestSandboxedExecutorDelegates(t *testing.T) {
	dir := t.TempDir()
	skill := createTestSkillWithHandler(t, dir, "#!/bin/sh\necho sandboxed")

	inner := NewScriptExecutor(5 * time.Second)
	e := NewSandboxedExecutor(inner)

	result, err := e.Execute(context.Background(), skill, "")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Output != "sandboxed\n" {
		t.Fatalf("got %q", result.Output)
	}
}

func TestExecutionResultDuration(t *testing.T) {
	dir := t.TempDir()
	skill := createTestSkillWithHandler(t, dir, "#!/bin/sh\necho fast")

	e := NewScriptExecutor(5 * time.Second)
	result, _ := e.Execute(context.Background(), skill, "")
	if result.Duration <= 0 {
		t.Fatal("expected positive duration")
	}
}
