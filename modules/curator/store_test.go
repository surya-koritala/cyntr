package curator

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRecordAndLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "curator.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	inv := Invocation{
		SkillName:  "code-review",
		Tenant:     "acme",
		Agent:      "reviewer",
		Success:    true,
		DurationMs: 123,
		Timestamp:  now,
	}
	if err := store.Record(inv); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, err := store.LoadInvocations("code-review", 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].SkillName != "code-review" {
		t.Fatalf("wrong skill: %s", got[0].SkillName)
	}
	if !got[0].Success {
		t.Fatal("expected success=true")
	}
	if got[0].DurationMs != 123 {
		t.Fatalf("expected 123ms, got %d", got[0].DurationMs)
	}
}

func TestStoreRecordWithError(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	judge := 0.42
	inv := Invocation{
		SkillName:     "incident-response",
		Tenant:        "acme",
		Agent:         "oncall",
		Success:       false,
		Error:         "timeout calling upstream",
		DurationMs:    9000,
		Timestamp:     time.Now().UTC(),
		LLMJudgeScore: &judge,
	}
	if err := store.Record(inv); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, err := store.LoadInvocations("incident-response", 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].Success {
		t.Fatal("expected failure")
	}
	if got[0].Error != "timeout calling upstream" {
		t.Fatalf("wrong error: %s", got[0].Error)
	}
	if got[0].LLMJudgeScore == nil || *got[0].LLMJudgeScore != 0.42 {
		t.Fatalf("expected judge=0.42, got %+v", got[0].LLMJudgeScore)
	}
}

func TestStoreListSkillNames(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	for _, n := range []string{"a", "b", "a", "c"} {
		if err := store.Record(Invocation{SkillName: n, Tenant: "t", Agent: "g", Success: true, DurationMs: 1, Timestamp: time.Now().UTC()}); err != nil {
			t.Fatalf("record %s: %v", n, err)
		}
	}
	names, err := store.ListSkillNames()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(names) != 3 {
		t.Fatalf("expected 3 distinct skills, got %d: %v", len(names), names)
	}
}
