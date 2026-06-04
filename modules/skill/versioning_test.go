package skill

import "testing"

func skillNamed(name, body string) *InstalledSkill {
	return &InstalledSkill{
		Manifest:     SkillManifest{Name: name, Version: "1.0.0"},
		Instructions: body,
	}
}

func TestReplaceWithBackupAndRollback(t *testing.T) {
	r := NewRegistry()
	if err := r.InstallDirect(skillNamed("greet", "v1 body")); err != nil {
		t.Fatalf("install: %v", err)
	}
	if r.VersionCount("greet") != 0 {
		t.Fatal("fresh install should have no prior versions")
	}

	// Replace with an improved version.
	if err := r.ReplaceWithBackup(skillNamed("greet", "v2 body")); err != nil {
		t.Fatalf("replace: %v", err)
	}
	got, _ := r.Get("greet")
	if got.Instructions != "v2 body" {
		t.Fatalf("live version = %q, want v2", got.Instructions)
	}
	if r.VersionCount("greet") != 1 {
		t.Fatalf("want 1 prior version, got %d", r.VersionCount("greet"))
	}

	// Rollback restores v1.
	if err := r.Rollback("greet"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	got, _ = r.Get("greet")
	if got.Instructions != "v1 body" {
		t.Fatalf("after rollback = %q, want v1", got.Instructions)
	}
	if r.VersionCount("greet") != 0 {
		t.Fatal("rollback should consume the backup")
	}
	if err := r.Rollback("greet"); err == nil {
		t.Fatal("rollback with no history should error")
	}
}

func TestReplaceWithBackupFreshInstall(t *testing.T) {
	r := NewRegistry()
	if err := r.ReplaceWithBackup(skillNamed("new", "body")); err != nil {
		t.Fatalf("replace on empty: %v", err)
	}
	if _, ok := r.Get("new"); !ok {
		t.Fatal("ReplaceWithBackup should install when name is new")
	}
}

func TestApprovingImprovedCandidateReplacesAndBacksUp(t *testing.T) {
	r, bus := startSkillRuntime(t, false)
	// Install an initial skill via a propose+approve.
	res1 := propose(t, bus, ProposeRequest{Tenant: "acme", Name: "diag", Instructions: "v1", SourceAgent: "a1"})
	if _, err := request(t, bus, TopicCandidateApprove, res1.ID); err != nil {
		t.Fatalf("approve v1: %v", err)
	}
	// Propose an improved version of the SAME skill and approve it.
	res2 := propose(t, bus, ProposeRequest{Tenant: "acme", Name: "diag", Instructions: "v2 improved", SourceAgent: "curator"})
	if _, err := request(t, bus, TopicCandidateApprove, res2.ID); err != nil {
		t.Fatalf("approve v2: %v", err)
	}
	got, _ := r.registry.Get("diag")
	if got.Instructions != "v2 improved" {
		t.Fatalf("live skill = %q, want v2", got.Instructions)
	}
	if r.registry.VersionCount("diag") != 1 {
		t.Fatalf("expected a backed-up prior version, got %d", r.registry.VersionCount("diag"))
	}
	// Rollback over the bus restores v1.
	if _, err := request(t, bus, TopicRollback, "diag"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	got, _ = r.registry.Get("diag")
	if got.Instructions != "v1" {
		t.Fatalf("after rollback = %q, want v1", got.Instructions)
	}
}
