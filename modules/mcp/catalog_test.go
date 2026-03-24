package mcp

import "testing"

func TestSearchBuiltinMCPCatalogEmpty(t *testing.T) {
	results := SearchBuiltinMCPCatalog("")
	if len(results) != len(BuiltinMCPCatalog) {
		t.Fatalf("expected all %d, got %d", len(BuiltinMCPCatalog), len(results))
	}
}

func TestSearchBuiltinMCPCatalogMatch(t *testing.T) {
	results := SearchBuiltinMCPCatalog("github")
	if len(results) == 0 {
		t.Fatal("expected results for github")
	}
}

func TestSearchBuiltinMCPCatalogNoMatch(t *testing.T) {
	results := SearchBuiltinMCPCatalog("nonexistent")
	if len(results) != 0 {
		t.Fatalf("expected 0, got %d", len(results))
	}
}

func TestBuiltinMCPCatalogHasEntries(t *testing.T) {
	if len(BuiltinMCPCatalog) < 5 {
		t.Fatalf("expected at least 5, got %d", len(BuiltinMCPCatalog))
	}
	for _, e := range BuiltinMCPCatalog {
		if e.Name == "" || e.Description == "" {
			t.Fatalf("entry missing fields: %+v", e)
		}
	}
}
