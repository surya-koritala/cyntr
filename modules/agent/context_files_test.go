package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeContextFile(t *testing.T, root, tenant, agent, name, content string) string {
	t.Helper()
	dir := filepath.Join(root, tenant, agent)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return p
}

func TestContextLoaderLoadsAndLabels(t *testing.T) {
	root := t.TempDir()
	writeContextFile(t, root, "acme", "assistant", "AGENTS.md", "Always answer in metric units.")
	writeContextFile(t, root, "acme", "assistant", "SOUL.md", "You are terse and kind.")

	out := NewContextLoader(root).Load("acme", "assistant")
	if !strings.Contains(out, "# AGENTS.md") || !strings.Contains(out, "metric units") {
		t.Fatalf("AGENTS.md not loaded/labeled: %q", out)
	}
	if !strings.Contains(out, "# SOUL.md") || !strings.Contains(out, "terse and kind") {
		t.Fatalf("SOUL.md not loaded/labeled: %q", out)
	}
}

func TestContextLoaderHotReload(t *testing.T) {
	root := t.TempDir()
	p := writeContextFile(t, root, "acme", "assistant", "AGENTS.md", "version one")
	cl := NewContextLoader(root)

	if out := cl.Load("acme", "assistant"); !strings.Contains(out, "version one") {
		t.Fatalf("first load wrong: %q", out)
	}
	// Rewrite with a strictly later mtime so the cache is invalidated.
	if err := os.WriteFile(p, []byte("version two"), 0o644); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	os.Chtimes(p, time.Now().Add(2*time.Second), time.Now().Add(2*time.Second))

	if out := cl.Load("acme", "assistant"); !strings.Contains(out, "version two") {
		t.Fatalf("edit not hot-reloaded: %q", out)
	}
}

func TestContextLoaderMissingIsEmptyNoError(t *testing.T) {
	cl := NewContextLoader(t.TempDir())
	if out := cl.Load("nobody", "nothing"); out != "" {
		t.Fatalf("missing workspace should yield empty, got %q", out)
	}
}

func TestContextLoaderRejectsTraversal(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ws")
	// Plant a secret as a sibling of the workspace root.
	secretDir := filepath.Join(filepath.Dir(root), "secret", "x")
	os.MkdirAll(secretDir, 0o755)
	os.WriteFile(filepath.Join(secretDir, "AGENTS.md"), []byte("TOP SECRET"), 0o644)

	cl := NewContextLoader(root)
	// tenant ".." tries to climb out of the workspace root.
	if out := cl.Load("../secret", "x"); out != "" {
		t.Fatalf("path traversal must be rejected, got %q", out)
	}
}

func TestContextLoaderNilSafe(t *testing.T) {
	var cl *ContextLoader
	if out := cl.Load("acme", "a"); out != "" {
		t.Fatal("nil loader should return empty")
	}
}
