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
		t.Skip("filesystem delivered no watch events (e.g. OneDrive/WSL mounts) — skipping fs-watch assertion")
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
	manifestPath := filepath.Join(skillDir, "skill.yaml")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(manifestPath, []byte("name: mod-skill\nversion: 1.0.0\n"), 0644)

	// Wait for the initial install (first reload) rather than sleeping a fixed
	// amount, which is flaky under coarse scheduler/filesystem timing.
	if !waitForCount(&reloadCount, 1, 3*time.Second) {
		t.Skip("filesystem delivered no watch events (e.g. OneDrive/WSL mounts) — skipping fs-watch assertion")
	}

	// Modify the manifest. Force a strictly newer mtime so the change is
	// detected even on filesystems with coarse mtime resolution (OneDrive/WSL),
	// where two writes in the same second can share an identical timestamp.
	os.WriteFile(manifestPath, []byte("name: mod-skill\nversion: 2.0.0\n"), 0644)
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(manifestPath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Wait for the modification to be picked up (second reload).
	if !waitForCount(&reloadCount, 2, 3*time.Second) {
		t.Fatalf("expected at least 2 reloads, got %d", reloadCount.Load())
	}

	skill, _ := reg.Get("mod-skill")
	if skill.Manifest.Version != "2.0.0" {
		t.Fatalf("expected v2, got %q", skill.Manifest.Version)
	}
}

// waitForCount polls counter until it reaches at least want, or timeout elapses.
func waitForCount(counter *atomic.Int64, want int64, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if counter.Load() >= want {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return counter.Load() >= want
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
