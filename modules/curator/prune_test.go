package curator

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// fakeDisabler is a test-only PruneSkillDisabler. It records calls
// and mimics the registry's "already disabled" error so the prune
// idempotency path is exercised.
type fakeDisabler struct {
	mu       sync.Mutex
	disabled map[string]string // skill -> reason
}

func newFakeDisabler() *fakeDisabler {
	return &fakeDisabler{disabled: make(map[string]string)}
}

func (f *fakeDisabler) Disable(name, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.disabled[name]; ok {
		return fmt.Errorf("skill %q already disabled", name)
	}
	f.disabled[name] = reason
	return nil
}

func (f *fakeDisabler) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.disabled)
}

func newPruneModule(t *testing.T, now time.Time) (*Module, *fakeDisabler, *ipc.Bus) {
	t.Helper()
	dir := t.TempDir()
	bus := ipc.NewBus()
	mod := New(filepath.Join(dir, "curator.db"))
	mod.now = func() time.Time { return now }
	if err := mod.Init(context.Background(), &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := mod.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	disabler := newFakeDisabler()
	mod.SetSkillDisabler(disabler)

	t.Cleanup(func() {
		mod.Stop(context.Background())
		bus.Close()
	})
	return mod, disabler, bus
}

func seedFailingSkill(t *testing.T, mod *Module, name string, now time.Time) {
	t.Helper()
	// 20 failures, oldest 10 days ago, newest a few hours ago — this
	// reproduces a skill that has been failing for >7 days.
	for i := 0; i < 20; i++ {
		_, err := mod.Store().RecordID(Invocation{
			SkillName:  name,
			Tenant:     "t",
			Agent:      "g",
			Success:    false,
			Error:      fmt.Sprintf("boom %d", i),
			DurationMs: 100,
			Timestamp:  now.Add(-time.Duration(10*24-i*12) * time.Hour),
		})
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
}

func TestPruneDisablesFailingSkill(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	mod, disabler, _ := newPruneModule(t, now)
	seedFailingSkill(t, mod, "doomed", now)

	report, err := mod.PruneFailingSkills(context.Background())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Skill != "doomed" {
		t.Fatalf("expected 1 entry for doomed, got %+v", report.Entries)
	}
	if !report.Entries[0].Disabled {
		t.Fatalf("expected disabled=true, got %+v", report.Entries[0])
	}
	if len(report.Entries[0].Samples) == 0 {
		t.Fatalf("expected failure samples, got none")
	}
	if disabler.Count() != 1 {
		t.Fatalf("expected disabler to have 1 entry, got %d", disabler.Count())
	}
}

func TestPruneIsIdempotent(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	mod, disabler, _ := newPruneModule(t, now)
	seedFailingSkill(t, mod, "doomed", now)

	if _, err := mod.PruneFailingSkills(context.Background()); err != nil {
		t.Fatalf("first prune: %v", err)
	}
	// Second pass: prune logic still flags the skill, but disabler
	// reports "already disabled" — report should reflect that.
	report, err := mod.PruneFailingSkills(context.Background())
	if err != nil {
		t.Fatalf("second prune: %v", err)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %+v", report.Entries)
	}
	if report.Entries[0].Disabled {
		t.Fatal("expected disabled=false on re-prune (already disabled)")
	}
	if report.Entries[0].Reason != "already disabled" {
		t.Fatalf("expected reason='already disabled', got %q", report.Entries[0].Reason)
	}
	if disabler.Count() != 1 {
		t.Fatalf("disabler count should still be 1, got %d", disabler.Count())
	}
}

func TestPruneEmitsSkillDisabledEvent(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	mod, _, bus := newPruneModule(t, now)
	seedFailingSkill(t, mod, "doomed", now)

	got := make(chan SkillDisabledEvent, 1)
	sub := bus.Subscribe("test", TopicSkillDisabled, func(msg ipc.Message) (ipc.Message, error) {
		if ev, ok := msg.Payload.(SkillDisabledEvent); ok {
			got <- ev
		}
		return ipc.Message{}, nil
	})
	defer sub.Cancel()

	if _, err := mod.PruneFailingSkills(context.Background()); err != nil {
		t.Fatalf("prune: %v", err)
	}
	select {
	case ev := <-got:
		if ev.Skill != "doomed" {
			t.Fatalf("expected event for doomed, got %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("never received skill.disabled event")
	}
}

func TestPruneIgnoresHealthySkills(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	mod, disabler, _ := newPruneModule(t, now)
	for i := 0; i < 30; i++ {
		mod.Store().RecordID(Invocation{
			SkillName:  "happy",
			Success:    true,
			DurationMs: 10,
			Timestamp:  now.Add(-time.Duration(i) * time.Hour),
		})
	}
	report, err := mod.PruneFailingSkills(context.Background())
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if len(report.Entries) != 0 {
		t.Fatalf("expected no entries, got %+v", report.Entries)
	}
	if disabler.Count() != 0 {
		t.Fatalf("expected no disables, got %d", disabler.Count())
	}
}

func TestPruneIPCTopic(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	mod, _, bus := newPruneModule(t, now)
	seedFailingSkill(t, mod, "doomed", now)

	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "api", Target: ModuleName, Topic: TopicPruneRun,
	})
	if err != nil {
		t.Fatalf("ipc request: %v", err)
	}
	report, ok := resp.Payload.(*PruneReport)
	if !ok {
		t.Fatalf("expected *PruneReport, got %T", resp.Payload)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %+v", report.Entries)
	}
}
