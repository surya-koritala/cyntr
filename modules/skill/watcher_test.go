package skill

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatcherDetectsNewSkill(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	reloaded := make(chan string, 1)

	w := NewWatcher(dir, reg, 100*time.Millisecond)
	w.SetOnChange(func(name string) { reloaded <- name })
	w.Start()
	defer w.Stop()

	// Create a skill
	skillDir := filepath.Join(dir, "new-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: new-skill\nversion: 1.0.0\n"), 0644)

	select {
	case name := <-reloaded:
		if name != "new-skill" {
			t.Fatalf("got %q", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for skill detection")
	}

	// Verify installed in registry
	_, ok := reg.Get("new-skill")
	if !ok {
		t.Fatal("skill not in registry")
	}
}

func TestWatcherDetectsModification(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	var reloadCount atomic.Int64

	w := NewWatcher(dir, reg, 100*time.Millisecond)
	w.SetOnChange(func(name string) { reloadCount.Add(1) })
	w.Start()
	defer w.Stop()

	skillDir := filepath.Join(dir, "mod-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: mod-skill\nversion: 1.0.0\n"), 0644)

	time.Sleep(300 * time.Millisecond)

	// Modify the manifest
	time.Sleep(100 * time.Millisecond) // ensure different mtime
	os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: mod-skill\nversion: 2.0.0\n"), 0644)

	time.Sleep(300 * time.Millisecond)

	if reloadCount.Load() < 2 {
		t.Fatalf("expected at least 2 reloads, got %d", reloadCount.Load())
	}

	skill, _ := reg.Get("mod-skill")
	if skill.Manifest.Version != "2.0.0" {
		t.Fatalf("expected v2, got %q", skill.Manifest.Version)
	}
}

func TestWatcherStopsCleanly(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()
	w := NewWatcher(dir, reg, 100*time.Millisecond)
	w.Start()
	w.Stop() // should not hang
}

func TestWatcherWatchedCount(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()

	// Pre-create skills
	for _, name := range []string{"a", "b"} {
		sd := filepath.Join(dir, name)
		os.MkdirAll(sd, 0755)
		os.WriteFile(filepath.Join(sd, "skill.yaml"), []byte("name: "+name+"\nversion: 1.0.0\n"), 0644)
	}

	w := NewWatcher(dir, reg, 100*time.Millisecond)
	w.Start()
	defer w.Stop()

	time.Sleep(300 * time.Millisecond)
	if w.WatchedCount() != 2 {
		t.Fatalf("expected 2, got %d", w.WatchedCount())
	}
}

func TestWatcherIgnoresNonDirs(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry()

	// Create a file (not a directory) — should be ignored
	os.WriteFile(filepath.Join(dir, "not-a-skill.txt"), []byte("hi"), 0644)

	w := NewWatcher(dir, reg, 100*time.Millisecond)
	w.Start()
	defer w.Stop()

	time.Sleep(300 * time.Millisecond)
	if w.WatchedCount() != 0 {
		t.Fatalf("expected 0, got %d", w.WatchedCount())
	}
}
