package curator

import (
	"path/filepath"
	"testing"
	"time"
)

func TestClassifyHealthInsufficient(t *testing.T) {
	invs := []Invocation{
		{Success: true}, {Success: true}, {Success: true}, {Success: true},
	}
	if got := ClassifyHealth(invs); got != HealthInsufficientData {
		t.Fatalf("expected insufficient_data, got %s", got)
	}
}

func TestClassifyHealthHealthy(t *testing.T) {
	// 18 successes / 2 failures over 20 = 90% → healthy
	var invs []Invocation
	for i := 0; i < 18; i++ {
		invs = append(invs, Invocation{Success: true})
	}
	for i := 0; i < 2; i++ {
		invs = append(invs, Invocation{Success: false})
	}
	if got := ClassifyHealth(invs); got != HealthHealthy {
		t.Fatalf("expected healthy, got %s", got)
	}
}

func TestClassifyHealthShaky(t *testing.T) {
	// 14/20 = 70% → shaky (between 50 and 80)
	var invs []Invocation
	for i := 0; i < 14; i++ {
		invs = append(invs, Invocation{Success: true})
	}
	for i := 0; i < 6; i++ {
		invs = append(invs, Invocation{Success: false})
	}
	if got := ClassifyHealth(invs); got != HealthShaky {
		t.Fatalf("expected shaky, got %s", got)
	}
}

func TestClassifyHealthFailing(t *testing.T) {
	// 5/20 = 25% → failing
	var invs []Invocation
	for i := 0; i < 5; i++ {
		invs = append(invs, Invocation{Success: true})
	}
	for i := 0; i < 15; i++ {
		invs = append(invs, Invocation{Success: false})
	}
	if got := ClassifyHealth(invs); got != HealthFailing {
		t.Fatalf("expected failing, got %s", got)
	}
}

func TestClassifyHealthBoundary80(t *testing.T) {
	// 16/20 = exactly 80% — spec says >80% is healthy, so 80% is shaky.
	var invs []Invocation
	for i := 0; i < 16; i++ {
		invs = append(invs, Invocation{Success: true})
	}
	for i := 0; i < 4; i++ {
		invs = append(invs, Invocation{Success: false})
	}
	if got := ClassifyHealth(invs); got != HealthShaky {
		t.Fatalf("expected shaky at 80%%, got %s", got)
	}
}

func TestComputeScoreRollingTrend(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	// 5 invocations in the last 7 days, 5 in the prior 7 days
	var invs []Invocation
	// Newest first
	for i := 0; i < 5; i++ {
		invs = append(invs, Invocation{
			SkillName:  "x",
			Success:    true, // 100% recent
			DurationMs: 100,
			Timestamp:  now.Add(-time.Duration(i+1) * 24 * time.Hour),
		})
	}
	for i := 0; i < 5; i++ {
		invs = append(invs, Invocation{
			SkillName:  "x",
			Success:    i%2 == 0, // 60% prior (3 of 5)
			DurationMs: 200,
			Timestamp:  now.Add(-time.Duration(8+i) * 24 * time.Hour),
		})
	}

	score := ComputeScore("x", invs, now)
	if score.Invocations != 10 {
		t.Fatalf("expected 10 invocations, got %d", score.Invocations)
	}
	if score.Last7dCount != 5 {
		t.Fatalf("expected 5 in last 7d, got %d", score.Last7dCount)
	}
	if score.Last7dRate != 100.0 {
		t.Fatalf("expected 100%% last7d rate, got %f", score.Last7dRate)
	}
	if score.Prior7dCount != 5 {
		t.Fatalf("expected 5 in prior 7d, got %d", score.Prior7dCount)
	}
	if score.Prior7dRate != 60.0 {
		t.Fatalf("expected 60%% prior7d rate, got %f", score.Prior7dRate)
	}
	// avg = (5*100 + 5*200)/10 = 150
	if score.AvgDurationMs != 150 {
		t.Fatalf("expected avg 150ms, got %f", score.AvgDurationMs)
	}
}

func TestComputePruneSuggestions(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	// "bad-skill": 20 invocations stretched over the past 10 days,
	// 1 success / 19 failures → failing for >7d.
	for i := 0; i < 20; i++ {
		ts := now.Add(-time.Duration(10*24-i*12) * time.Hour) // span ~10d
		if err := store.Record(Invocation{
			SkillName:  "bad-skill",
			Tenant:     "t", Agent: "g",
			Success:    i == 0, // only first (oldest) succeeded
			DurationMs: 100,
			Timestamp:  ts,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	// "good-skill": 20 successful invocations, recent → healthy.
	for i := 0; i < 20; i++ {
		if err := store.Record(Invocation{
			SkillName:  "good-skill",
			Tenant:     "t", Agent: "g",
			Success:    true,
			DurationMs: 50,
			Timestamp:  now.Add(-time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	suggestions, err := ComputePruneSuggestions(store, now)
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d: %+v", len(suggestions), suggestions)
	}
	if suggestions[0].SkillName != "bad-skill" {
		t.Fatalf("expected bad-skill, got %s", suggestions[0].SkillName)
	}
	if suggestions[0].FailingForDays < 7 {
		t.Fatalf("expected >=7 days failing, got %f", suggestions[0].FailingForDays)
	}
}

func TestComputeAllScoresOrdered(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC()
	for _, n := range []string{"zebra", "alpha", "mango"} {
		if err := store.Record(Invocation{SkillName: n, Tenant: "t", Agent: "g", Success: true, DurationMs: 1, Timestamp: now}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	scores, err := ComputeAllScores(store, now)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if len(scores) != 3 {
		t.Fatalf("expected 3, got %d", len(scores))
	}
	if scores[0].SkillName != "alpha" || scores[2].SkillName != "zebra" {
		t.Fatalf("expected alpha…zebra ordering, got %v", []string{scores[0].SkillName, scores[1].SkillName, scores[2].SkillName})
	}
}
