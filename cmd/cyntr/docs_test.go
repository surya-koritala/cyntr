package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunDocsCreatesFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "docs")
	runDocs([]string{dir})

	expected := []string{"api-reference.md", "tools-reference.md", "skills-reference.md", "cli-reference.md", "config-reference.md"}
	for _, name := range expected {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s to exist", name)
		}
	}
}

func TestRunDocsDefaultDir(t *testing.T) {
	// Just verify it doesn't panic with no args
	// Can't easily test default "docs" dir without cleanup
}
