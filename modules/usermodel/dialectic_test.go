package usermodel

import (
	"context"
	"fmt"
	"testing"
)

// newFactDistiller builds a distiller wired to a fakeProvider with facts on,
// over a tenant that has distillation enabled and enough seeded activity.
func newFactDistiller(t *testing.T, fp *fakeProvider, audit AuditEmitter) (*Distiller, *Store) {
	t.Helper()
	s := newTestStoreForDistill(t)
	if err := s.SetTenantDistillEnabled("acme", true); err != nil {
		t.Fatalf("enable tenant: %v", err)
	}
	seedActivity(t, s, "acme", "jane", 4)
	d, err := NewDistiller(DistillerOptions{Store: s, Provider: fp, Model: "m", Audit: audit, EnableFacts: true})
	if err != nil {
		t.Fatalf("NewDistiller: %v", err)
	}
	return d, s
}

func TestDistillFactsAddRevisRetireAcrossSessions(t *testing.T) {
	fp := &fakeProvider{response: `[{"op":"add","fact":"Prefers Go","confidence":0.7}]`}
	d, s := newFactDistiller(t, fp, nil)
	ctx := context.Background()

	// Session 1: add.
	res, err := d.DistillFacts(ctx, "acme", "jane")
	if err != nil {
		t.Fatalf("DistillFacts add: %v", err)
	}
	if res.Added != 1 {
		t.Fatalf("added = %d, want 1", res.Added)
	}
	facts, _ := s.ActiveFacts("acme", "jane")
	if len(facts) != 1 || facts[0].Text != "Prefers Go" {
		t.Fatalf("fact not stored: %+v", facts)
	}
	id := facts[0].ID

	// Session 2: revise confidence up.
	fp.response = fmt.Sprintf(`[{"op":"revise","id":%d,"confidence":0.95}]`, id)
	res, _ = d.DistillFacts(ctx, "acme", "jane")
	if res.Revised != 1 {
		t.Fatalf("revised = %d, want 1", res.Revised)
	}
	facts, _ = s.ActiveFacts("acme", "jane")
	if facts[0].Confidence != 0.95 {
		t.Fatalf("confidence not revised: %v", facts[0].Confidence)
	}

	// Session 3: retire.
	fp.response = fmt.Sprintf(`[{"op":"retire","id":%d}]`, id)
	res, _ = d.DistillFacts(ctx, "acme", "jane")
	if res.Retired != 1 {
		t.Fatalf("retired = %d, want 1", res.Retired)
	}
	if facts, _ := s.ActiveFacts("acme", "jane"); len(facts) != 0 {
		t.Fatalf("fact should be retired, still active: %+v", facts)
	}
}

func TestDistillFactsGates(t *testing.T) {
	// Tenant not enabled -> skipped.
	fp := &fakeProvider{response: `[]`}
	s := newTestStoreForDistill(t)
	seedActivity(t, s, "acme", "jane", 4)
	d, _ := NewDistiller(DistillerOptions{Store: s, Provider: fp, EnableFacts: true})
	res, _ := d.DistillFacts(context.Background(), "acme", "jane")
	if !res.Skipped || res.SkipReason != "tenant_distill_disabled" {
		t.Fatalf("expected tenant gate skip, got %+v", res)
	}
	if fp.callCount() != 0 {
		t.Fatal("provider should not be called when gated")
	}

	// Enabled but too little activity -> skipped.
	s.SetTenantDistillEnabled("acme", true)
	s2 := newTestStoreForDistill(t)
	s2.SetTenantDistillEnabled("acme", true)
	seedActivity(t, s2, "acme", "jane", 1)
	d2, _ := NewDistiller(DistillerOptions{Store: s2, Provider: fp, EnableFacts: true})
	res, _ = d2.DistillFacts(context.Background(), "acme", "jane")
	if !res.Skipped || res.SkipReason != "insufficient_sessions" {
		t.Fatalf("expected insufficient_sessions skip, got %+v", res)
	}
}

func TestDistillFactsEmitsAudit(t *testing.T) {
	audit := &captureAudit{}
	fp := &fakeProvider{response: `[{"op":"add","fact":"Likes tests","confidence":0.8}]`}
	d, _ := newFactDistiller(t, fp, audit)
	d.DistillFacts(context.Background(), "acme", "jane")

	var sawAdd bool
	for _, e := range audit.all() {
		if e.Action == "usermodel.fact_add" && e.Status == "success" {
			sawAdd = true
		}
	}
	if !sawAdd {
		t.Fatal("expected a usermodel.fact_add audit entry")
	}
}

func TestDistillFactsBoundsGrowth(t *testing.T) {
	fp := &fakeProvider{response: `[]`} // model proposes nothing new
	d, s := newFactDistiller(t, fp, nil)
	// Pre-load more than the cap.
	for i := 0; i < MaxActiveFacts+5; i++ {
		s.AddFact("acme", "jane", fmt.Sprintf("fact %d", i), 0.5, "s1")
	}
	d.DistillFacts(context.Background(), "acme", "jane")
	n, _ := s.CountActiveFacts("acme", "jane")
	if n != MaxActiveFacts {
		t.Fatalf("active facts = %d, want capped at %d", n, MaxActiveFacts)
	}
}

func TestDistillFactsCannotTargetForeignFacts(t *testing.T) {
	// A fact belongs to acme/jane; the model (running for globex/jane) returns
	// a revise op against acme's id. Scoping must reject it.
	fp := &fakeProvider{response: `[{"op":"add","fact":"acme only","confidence":0.7}]`}
	d, s := newFactDistiller(t, fp, nil)
	d.DistillFacts(context.Background(), "acme", "jane")
	acmeFacts, _ := s.ActiveFacts("acme", "jane")
	foreignID := acmeFacts[0].ID

	s.SetTenantDistillEnabled("globex", true)
	seedActivity(t, s, "globex", "jane", 4)
	fp.response = fmt.Sprintf(`[{"op":"retire","id":%d}]`, foreignID)
	res, _ := d.DistillFacts(context.Background(), "globex", "jane")
	if res.Retired != 0 {
		t.Fatalf("globex should not retire acme's fact, retired=%d", res.Retired)
	}
	if facts, _ := s.ActiveFacts("acme", "jane"); len(facts) != 1 {
		t.Fatalf("acme's fact was tampered with cross-tenant: %+v", facts)
	}
}

func TestDistillUserRunsFactsWhenEnabled(t *testing.T) {
	// With EnableFacts, the normal distill path also runs the facts pass. The
	// fakeProvider returns a JSON array, so the facts pass parses + applies it.
	fp := &fakeProvider{response: `[{"op":"add","fact":"From the distill path","confidence":0.6}]`}
	d, s := newFactDistiller(t, fp, nil)
	if _, err := d.DistillUserForce(context.Background(), "acme", "jane"); err != nil {
		t.Fatalf("DistillUserForce: %v", err)
	}
	facts, _ := s.ActiveFacts("acme", "jane")
	if len(facts) != 1 || facts[0].Text != "From the distill path" {
		t.Fatalf("facts pass did not run via distillUser: %+v", facts)
	}
}

func TestParseFactDeltas(t *testing.T) {
	cases := []struct {
		in    string
		count int
	}{
		{"[]", 0},
		{`[{"op":"add","fact":"x","confidence":0.5}]`, 1},
		{"```json\n[{\"op\":\"retire\",\"id\":3}]\n```", 1},
		{"Sure! Here are the updates:\n[{\"op\":\"add\",\"fact\":\"y\",\"confidence\":0.2}]\nDone.", 1},
		{"I cannot help with that.", 0}, // no array -> no changes, no error
	}
	for _, c := range cases {
		got, err := parseFactDeltas(c.in)
		if err != nil {
			t.Fatalf("parse %q: %v", c.in, err)
		}
		if len(got) != c.count {
			t.Fatalf("parse %q: got %d deltas, want %d", c.in, len(got), c.count)
		}
	}
}
