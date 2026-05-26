package curator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// staticSnapshotter is a test ConsolidationSnapshotter that just
// returns the canned list it was constructed with.
type staticSnapshotter struct {
	skills []ConsolidationSkillSnapshot
}

func (s *staticSnapshotter) SkillsForConsolidation() []ConsolidationSkillSnapshot {
	return s.skills
}

func newConsolidateModule(t *testing.T) (*Module, *ipc.Bus) {
	t.Helper()
	dir := t.TempDir()
	bus := ipc.NewBus()
	mod := New(filepath.Join(dir, "curator.db"))
	mod.now = func() time.Time { return time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC) }
	if err := mod.Init(context.Background(), &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := mod.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		mod.Stop(context.Background())
		bus.Close()
	})
	return mod, bus
}

func TestConsolidateDetectsHighOverlap(t *testing.T) {
	mod, _ := newConsolidateModule(t)
	mod.SetSnapshotter(&staticSnapshotter{
		skills: []ConsolidationSkillSnapshot{
			{Name: "code-review", Tools: []string{"grep", "read", "bash"}, Invocations: 100},
			{Name: "code-audit", Tools: []string{"grep", "read", "bash", "diff"}, Invocations: 80},
			{Name: "weather", Tools: []string{"http"}, Invocations: 200},
		},
	})

	report, err := mod.SuggestConsolidation(context.Background())
	if err != nil {
		t.Fatalf("consolidate: %v", err)
	}
	if len(report.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d: %+v", len(report.Suggestions), report.Suggestions)
	}
	got := report.Suggestions[0]
	// Jaccard for {grep,read,bash} ∩ {grep,read,bash,diff} = 3 / 4 = 0.75
	if got.Jaccard < 0.74 || got.Jaccard > 0.76 {
		t.Fatalf("expected jaccard ~0.75, got %f", got.Jaccard)
	}
	if len(got.SharedTools) != 3 {
		t.Fatalf("expected 3 shared tools, got %v", got.SharedTools)
	}
}

func TestConsolidateIgnoresLowInvocationSkills(t *testing.T) {
	mod, _ := newConsolidateModule(t)
	mod.SetSnapshotter(&staticSnapshotter{
		skills: []ConsolidationSkillSnapshot{
			// Both skills overlap heavily, but one has fewer than 50
			// invocations — should be ignored.
			{Name: "popular", Tools: []string{"a", "b", "c"}, Invocations: 100},
			{Name: "unloved", Tools: []string{"a", "b", "c"}, Invocations: 5},
		},
	})

	report, err := mod.SuggestConsolidation(context.Background())
	if err != nil {
		t.Fatalf("consolidate: %v", err)
	}
	if len(report.Suggestions) != 0 {
		t.Fatalf("expected no suggestions when one skill below floor, got %+v", report.Suggestions)
	}
}

func TestConsolidateIgnoresLowOverlap(t *testing.T) {
	mod, _ := newConsolidateModule(t)
	mod.SetSnapshotter(&staticSnapshotter{
		skills: []ConsolidationSkillSnapshot{
			{Name: "a", Tools: []string{"x", "y", "z"}, Invocations: 100},
			{Name: "b", Tools: []string{"x", "p", "q", "r"}, Invocations: 100},
		},
	})
	report, err := mod.SuggestConsolidation(context.Background())
	if err != nil {
		t.Fatalf("consolidate: %v", err)
	}
	// Jaccard = 1 / 6 ≈ 0.167 < 0.6 → no suggestion.
	if len(report.Suggestions) != 0 {
		t.Fatalf("expected no suggestions at low overlap, got %+v", report.Suggestions)
	}
}

func TestConsolidateNoSnapshotterReturnsEmpty(t *testing.T) {
	mod, _ := newConsolidateModule(t)
	// no snapshotter wired
	report, err := mod.SuggestConsolidation(context.Background())
	if err != nil {
		t.Fatalf("consolidate: %v", err)
	}
	if len(report.Suggestions) != 0 {
		t.Fatalf("expected empty report, got %+v", report.Suggestions)
	}
}

func TestConsolidateIPCTopic(t *testing.T) {
	mod, bus := newConsolidateModule(t)
	mod.SetSnapshotter(&staticSnapshotter{
		skills: []ConsolidationSkillSnapshot{
			{Name: "a", Tools: []string{"x", "y", "z"}, Invocations: 100},
			{Name: "b", Tools: []string{"x", "y", "z"}, Invocations: 100},
		},
	})
	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "api", Target: ModuleName, Topic: TopicConsolidateRun,
	})
	if err != nil {
		t.Fatalf("ipc: %v", err)
	}
	report, ok := resp.Payload.(*ConsolidationReport)
	if !ok {
		t.Fatalf("expected *ConsolidationReport, got %T", resp.Payload)
	}
	if len(report.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %+v", report.Suggestions)
	}
}
