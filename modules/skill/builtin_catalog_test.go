package skill

import "testing"

func TestSearchBuiltinCatalogEmpty(t *testing.T) {
	results := SearchBuiltinCatalog("")
	if len(results) != len(BuiltinCatalog) {
		t.Fatalf("empty query should return all %d skills, got %d", len(BuiltinCatalog), len(results))
	}
}

func TestSearchBuiltinCatalogMatch(t *testing.T) {
	results := SearchBuiltinCatalog("cloud")
	if len(results) == 0 {
		t.Fatal("expected results for 'cloud'")
	}
	for _, r := range results {
		if r.Name == "" {
			t.Fatal("result should have a name")
		}
	}
}

func TestSearchBuiltinCatalogNoMatch(t *testing.T) {
	results := SearchBuiltinCatalog("xyznonexistent")
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestBuiltinCatalogHasEntries(t *testing.T) {
	if len(BuiltinCatalog) < 5 {
		t.Fatalf("expected at least 5 built-in skills, got %d", len(BuiltinCatalog))
	}
	for _, entry := range BuiltinCatalog {
		if entry.Name == "" || entry.Description == "" || entry.Author == "" {
			t.Fatalf("catalog entry missing required fields: %+v", entry)
		}
	}
}
