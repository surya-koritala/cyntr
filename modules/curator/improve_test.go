package curator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// improveFakeProvider returns a fixed improved body.
type improveFakeProvider struct{ body string }

func (p *improveFakeProvider) Name() string { return "fake" }
func (p *improveFakeProvider) Chat(ctx context.Context, msgs []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	return agent.Message{Role: agent.RoleAssistant, Content: p.body}, nil
}

func newImproveStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "curator.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedFailures(t *testing.T, s *Store, skill string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := s.Record(Invocation{
			SkillName: skill, Tenant: "acme", Agent: "a1", Success: false,
			Error: "boom", DurationMs: 10, Timestamp: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
}

func TestImproverProposesFromFailures(t *testing.T) {
	s := newImproveStore(t)
	seedFailures(t, s, "diag", 3)

	var proposedName, proposedBody string
	fetch := func(name string) (string, error) { return "old instructions", nil }
	propose := func(name, desc, instr string) error {
		proposedName, proposedBody = name, instr
		return nil
	}
	im := NewImprover(&improveFakeProvider{body: "# Improved\nbetter steps"}, "m", fetch, propose)

	proposed, err := im.Improve(context.Background(), s, "diag")
	if err != nil {
		t.Fatalf("Improve: %v", err)
	}
	if !proposed {
		t.Fatal("expected a proposal for a failing skill with samples")
	}
	if proposedName != "diag" || proposedBody != "# Improved\nbetter steps" {
		t.Fatalf("proposal wrong: name=%q body=%q", proposedName, proposedBody)
	}
}

func TestImproverNoFailuresNoProposal(t *testing.T) {
	s := newImproveStore(t)
	// No failure samples recorded.
	called := false
	im := NewImprover(&improveFakeProvider{body: "x"}, "m",
		func(string) (string, error) { return "cur", nil },
		func(string, string, string) error { called = true; return nil })

	proposed, err := im.Improve(context.Background(), s, "diag")
	if err != nil {
		t.Fatalf("Improve: %v", err)
	}
	if proposed || called {
		t.Fatal("no failures -> no proposal")
	}
}

func TestImproveRunScanProposesForFailingOnly(t *testing.T) {
	// Build a module with a store + improver directly, bypassing kernel wiring.
	store := newImproveStore(t)
	m2 := &Module{store: store, now: func() time.Time { return time.Now().UTC() }}

	// "healthy" skill: 20 successes. "broken" skill: 10 failures (<50%).
	for i := 0; i < 20; i++ {
		store.Record(Invocation{SkillName: "healthy", Tenant: "acme", Success: true, Timestamp: time.Now().UTC()})
	}
	seedFailures(t, store, "broken", 10)

	var proposed []string
	m2.improver = NewImprover(&improveFakeProvider{body: "fixed"}, "m",
		func(string) (string, error) { return "cur", nil },
		func(name, _, _ string) error { proposed = append(proposed, name); return nil })

	got, err := m2.runImproveScan(context.Background())
	if err != nil {
		t.Fatalf("runImproveScan: %v", err)
	}
	if len(got) != 1 || got[0] != "broken" {
		t.Fatalf("scan should improve only the failing skill, got %v", got)
	}
	if len(proposed) != 1 || proposed[0] != "broken" {
		t.Fatalf("proposal raised for wrong skills: %v", proposed)
	}
}
