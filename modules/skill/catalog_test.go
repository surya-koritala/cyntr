package skill

import "testing"

func TestLoadEmbeddedCatalog(t *testing.T) {
	skills := LoadEmbeddedCatalog()
	// Should load at least the README placeholder
	if skills == nil {
		t.Fatal("expected non-nil skills from embedded catalog")
	}
}

func TestCatalogSkillsHaveNames(t *testing.T) {
	skills := LoadEmbeddedCatalog()
	for _, s := range skills {
		if s.Manifest.Name == "" {
			t.Fatal("catalog skill missing name")
		}
	}
}
