package agent

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func newPersonalityStore(t *testing.T) *PersonalityStore {
	t.Helper()
	ps, err := NewPersonalityStore(filepath.Join(t.TempDir(), "personalities.db"))
	if err != nil {
		t.Fatalf("NewPersonalityStore: %v", err)
	}
	t.Cleanup(func() { ps.Close() })
	return ps
}

func TestPersonalitySeedDefaults(t *testing.T) {
	ps := newPersonalityStore(t)
	if err := ps.SeedDefaults("acme"); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	names := ps.PersonalityNames("acme")
	for _, want := range []string{"default", "concise", "friendly"} {
		found := false
		for _, n := range names {
			if n == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("default %q missing from %v", want, names)
		}
	}

	// Seeding again must not error or duplicate, and must not clobber an edit.
	if err := ps.Save(Personality{Tenant: "acme", Name: "concise", Prompt: "EDITED"}); err != nil {
		t.Fatalf("Save edit: %v", err)
	}
	// Reset seeded flag to force re-seed path.
	ps.mu.Lock()
	ps.seeded["acme"] = false
	ps.mu.Unlock()
	if err := ps.SeedDefaults("acme"); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	got, err := ps.Get("acme", "concise")
	if err != nil {
		t.Fatalf("Get after re-seed: %v", err)
	}
	if got.Prompt != "EDITED" {
		t.Fatalf("re-seed clobbered tenant edit: %q", got.Prompt)
	}
}

func TestPersonalityCRUD(t *testing.T) {
	ps := newPersonalityStore(t)
	if err := ps.Save(Personality{Tenant: "acme", Name: "Pirate", Prompt: "Arr, talk like a pirate."}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Name is normalized (case-insensitive).
	got, err := ps.Get("acme", "pirate")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Prompt != "Arr, talk like a pirate." || got.Name != "pirate" {
		t.Fatalf("unexpected: %+v", got)
	}

	// Update.
	if err := ps.Save(Personality{Tenant: "acme", Name: "pirate", Prompt: "Arr, even saltier."}); err != nil {
		t.Fatalf("update Save: %v", err)
	}
	got, _ = ps.Get("acme", "pirate")
	if got.Prompt != "Arr, even saltier." {
		t.Fatalf("update did not take: %+v", got)
	}

	// Delete.
	if err := ps.Delete("acme", "pirate"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := ps.Get("acme", "pirate"); !errors.Is(err, ErrUnknownPersonality) {
		t.Fatalf("expected ErrUnknownPersonality after delete, got %v", err)
	}

	// Missing fields error.
	if err := ps.Save(Personality{Tenant: "acme", Name: "x", Prompt: "  "}); err == nil {
		t.Fatalf("expected error for empty prompt")
	}
	if err := ps.Save(Personality{Tenant: "", Name: "x", Prompt: "y"}); err == nil {
		t.Fatalf("expected error for empty tenant")
	}
}

func TestPersonalityUnknownNameErrors(t *testing.T) {
	ps := newPersonalityStore(t)
	ps.SeedDefaults("acme")

	if _, err := ps.Get("acme", "nope"); !errors.Is(err, ErrUnknownPersonality) {
		t.Fatalf("Get unknown: expected ErrUnknownPersonality, got %v", err)
	}
	if _, err := ps.Select("acme", "sess1", "nope"); !errors.Is(err, ErrUnknownPersonality) {
		t.Fatalf("Select unknown: expected ErrUnknownPersonality, got %v", err)
	}
	// A failed Select must not change the active selection.
	if name := ps.ActiveName("acme", "sess1"); name != "" {
		t.Fatalf("failed Select set active to %q", name)
	}
}

func TestPersonalitySelectionChangesPromptAndPersists(t *testing.T) {
	ps := newPersonalityStore(t)
	ps.SeedDefaults("acme")

	const sess = "sess_acme_bot"
	base := "# AGENTS.md\n\nProject conventions here."

	// No selection: Compose returns the prelude unchanged.
	if out := ps.Compose("acme", sess, base); out != base {
		t.Fatalf("no-selection Compose changed prelude: %q", out)
	}

	// Select concise.
	if _, err := ps.Select("acme", sess, "concise"); err != nil {
		t.Fatalf("Select concise: %v", err)
	}
	out1 := ps.Compose("acme", sess, base)
	if !strings.Contains(out1, "extremely concise") {
		t.Fatalf("concise prompt not composed: %q", out1)
	}
	if !strings.Contains(out1, "AGENTS.md") {
		t.Fatalf("Compose dropped the existing prelude (must compose, not replace): %q", out1)
	}
	// Persona must come first (prepended).
	if strings.Index(out1, "concise") > strings.Index(out1, "AGENTS.md") {
		t.Fatalf("persona not prepended ahead of prelude: %q", out1)
	}

	// Switch to friendly: assembled prompt changes.
	if _, err := ps.Select("acme", sess, "friendly"); err != nil {
		t.Fatalf("Select friendly: %v", err)
	}
	out2 := ps.Compose("acme", sess, base)
	if out2 == out1 {
		t.Fatalf("switching persona did not change composed prompt")
	}
	if !strings.Contains(out2, "friendly") {
		t.Fatalf("friendly prompt not composed: %q", out2)
	}

	// Persists for the session: a second Compose call (a later turn) still uses friendly.
	if again := ps.Compose("acme", sess, base); again != out2 {
		t.Fatalf("selection did not persist across turns: %q vs %q", again, out2)
	}

	// A different session in the same tenant has its own (empty) selection.
	if out := ps.Compose("acme", "sess_other", base); out != base {
		t.Fatalf("selection leaked across sessions: %q", out)
	}
}

func TestPersonalityTenantCatalogIsolation(t *testing.T) {
	ps := newPersonalityStore(t)
	ps.SeedDefaults("acme")
	if err := ps.Save(Personality{Tenant: "acme", Name: "pirate", Prompt: "arr"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// globex must not see acme's custom persona.
	if _, err := ps.Get("globex", "pirate"); !errors.Is(err, ErrUnknownPersonality) {
		t.Fatalf("cross-tenant Get leaked: %v", err)
	}
	if _, err := ps.Select("globex", "s1", "pirate"); !errors.Is(err, ErrUnknownPersonality) {
		t.Fatalf("cross-tenant Select leaked: %v", err)
	}
	for _, n := range ps.PersonalityNames("globex") {
		if n == "pirate" {
			t.Fatalf("pirate leaked into globex catalog")
		}
	}

	// Same persona name in two tenants is independent.
	ps.Save(Personality{Tenant: "globex", Name: "pirate", Prompt: "yarr different"})
	a, _ := ps.Get("acme", "pirate")
	g, _ := ps.Get("globex", "pirate")
	if a.Prompt == g.Prompt {
		t.Fatalf("tenant catalogs not isolated: both %q", a.Prompt)
	}
}

func TestPersonalityActivePromptStaleSelectionDegrades(t *testing.T) {
	ps := newPersonalityStore(t)
	ps.SeedDefaults("acme")
	const sess = "sess_acme_bot"
	ps.Select("acme", sess, "concise")
	// Delete the active persona out from under the session.
	ps.Delete("acme", "concise")
	// Compose must not fail — degrades to no fragment.
	if out := ps.Compose("acme", sess, "base"); out != "base" {
		t.Fatalf("stale selection should degrade to prelude, got %q", out)
	}
}

func TestParsePersonalityCommand(t *testing.T) {
	tests := []struct {
		in       string
		wantName string
		wantOK   bool
	}{
		{"/personality concise", "concise", true},
		{"  /personality   friendly  ", "friendly", true},
		{"/persona pirate", "pirate", true},
		{"/personality", "", true},
		{"/persona", "", true},
		{"hello there", "", false},
		{"/personalitything", "", false},
		{"tell me about /personality", "", false},
	}
	for _, tt := range tests {
		name, ok := ParsePersonalityCommand(tt.in)
		if ok != tt.wantOK || name != tt.wantName {
			t.Errorf("ParsePersonalityCommand(%q) = (%q,%v), want (%q,%v)", tt.in, name, ok, tt.wantName, tt.wantOK)
		}
	}
}
