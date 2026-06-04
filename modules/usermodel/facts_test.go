package usermodel

import "testing"

func TestAddAndActiveFactsOrdering(t *testing.T) {
	s := newTestStore(t)
	_, _ = s.AddFact("acme", "jane", "prefers Go", 0.6, "s1")
	hi, _ := s.AddFact("acme", "jane", "drinks coffee", 0.9, "s1")

	facts, err := s.ActiveFacts("acme", "jane")
	if err != nil {
		t.Fatalf("ActiveFacts: %v", err)
	}
	if len(facts) != 2 {
		t.Fatalf("want 2 facts, got %d", len(facts))
	}
	if facts[0].ID != hi {
		t.Fatalf("highest-confidence fact should sort first, got %+v", facts[0])
	}
	if facts[0].Confidence != 0.9 || facts[0].Text != "drinks coffee" {
		t.Fatalf("unexpected top fact: %+v", facts[0])
	}
}

func TestReviseFact(t *testing.T) {
	s := newTestStore(t)
	id, _ := s.AddFact("acme", "jane", "prefers Go", 0.5, "s1")

	if err := s.ReviseFact("acme", "jane", id, "strongly prefers Go", 0.9); err != nil {
		t.Fatalf("ReviseFact: %v", err)
	}
	facts, _ := s.ActiveFacts("acme", "jane")
	if facts[0].Text != "strongly prefers Go" || facts[0].Confidence != 0.9 {
		t.Fatalf("revise did not apply: %+v", facts[0])
	}

	// Blank text revises confidence only.
	if err := s.ReviseFact("acme", "jane", id, "", 0.3); err != nil {
		t.Fatalf("ReviseFact conf-only: %v", err)
	}
	facts, _ = s.ActiveFacts("acme", "jane")
	if facts[0].Text != "strongly prefers Go" || facts[0].Confidence != 0.3 {
		t.Fatalf("conf-only revise wrong: %+v", facts[0])
	}
}

func TestRetiredFactsExcluded(t *testing.T) {
	s := newTestStore(t)
	keep, _ := s.AddFact("acme", "jane", "keep me", 0.8, "s1")
	drop, _ := s.AddFact("acme", "jane", "retire me", 0.4, "s1")

	if err := s.RetireFact("acme", "jane", drop); err != nil {
		t.Fatalf("RetireFact: %v", err)
	}
	facts, _ := s.ActiveFacts("acme", "jane")
	if len(facts) != 1 || facts[0].ID != keep {
		t.Fatalf("retired fact still active: %+v", facts)
	}
	if n, _ := s.CountActiveFacts("acme", "jane"); n != 1 {
		t.Fatalf("CountActiveFacts = %d, want 1", n)
	}
}

func TestFactsTenantUserIsolation(t *testing.T) {
	s := newTestStore(t)
	mine, _ := s.AddFact("acme", "jane", "shared text", 0.7, "s1")
	s.AddFact("globex", "jane", "shared text", 0.7, "s1")
	s.AddFact("acme", "bob", "shared text", 0.7, "s1")

	facts, _ := s.ActiveFacts("acme", "jane")
	if len(facts) != 1 {
		t.Fatalf("isolation broken: acme/jane sees %d facts", len(facts))
	}

	// Revising under the wrong tenant must fail and leave the row untouched.
	if err := s.ReviseFact("globex", "jane", mine, "hacked", 0.0); err == nil {
		t.Fatal("cross-tenant ReviseFact should fail")
	}
	// Retiring under the wrong user must be rejected, not silently applied.
	if err := s.RetireFact("acme", "bob", mine); err == nil {
		t.Fatal("cross-user RetireFact should be rejected")
	}
	if facts, _ := s.ActiveFacts("acme", "jane"); len(facts) != 1 || facts[0].Text != "shared text" {
		t.Fatalf("foreign retire affected the wrong row: %+v", facts)
	}
}

func TestRetireLowestConfidenceCap(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 5; i++ {
		// confidences 0.1, 0.3, 0.5, 0.7, 0.9
		s.AddFact("acme", "jane", "fact", float64(i)*0.2+0.1, "s1")
	}
	retired, err := s.RetireLowestConfidence("acme", "jane", 3)
	if err != nil {
		t.Fatalf("RetireLowestConfidence: %v", err)
	}
	if retired != 2 {
		t.Fatalf("retired %d, want 2", retired)
	}
	facts, _ := s.ActiveFacts("acme", "jane")
	if len(facts) != 3 {
		t.Fatalf("want 3 active after cap, got %d", len(facts))
	}
	// The survivors are the three highest-confidence facts.
	if facts[0].Confidence < facts[2].Confidence || facts[2].Confidence < 0.5 {
		t.Fatalf("cap kept the wrong facts: %+v", facts)
	}
}

func TestConfidenceClamped(t *testing.T) {
	s := newTestStore(t)
	s.AddFact("acme", "jane", "over", 2.5, "s1")
	s.AddFact("acme", "jane", "under", -1.0, "s1")
	facts, _ := s.ActiveFacts("acme", "jane")
	for _, f := range facts {
		if f.Confidence < 0 || f.Confidence > 1 {
			t.Fatalf("confidence not clamped: %v", f.Confidence)
		}
	}
}
