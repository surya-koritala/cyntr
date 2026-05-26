package curator

import (
	"sort"
	"time"
)

// healthWindow is the number of most-recent invocations used by
// ClassifyHealth. Fixed at 20 per spec.
const healthWindow = 20

// pruneFailingFor is how long a skill must continuously be
// "failing" before the curator suggests pruning it. 7 days per spec.
const pruneFailingFor = 7 * 24 * time.Hour

// ClassifyHealth implements the spec's rules verbatim:
//
//	healthy = >80% success over last 20 invocations
//	shaky   = 50-80%
//	failing = <50%
//	<5 invocations = "insufficient_data"
//
// The caller must pass invocations newest-first; only the first
// healthWindow entries are considered.
func ClassifyHealth(invocations []Invocation) string {
	if len(invocations) < 5 {
		return HealthInsufficientData
	}
	window := invocations
	if len(window) > healthWindow {
		window = window[:healthWindow]
	}
	successes := 0
	for _, inv := range window {
		if inv.Success {
			successes++
		}
	}
	rate := float64(successes) / float64(len(window))
	switch {
	case rate > 0.80:
		return HealthHealthy
	case rate >= 0.50:
		return HealthShaky
	default:
		return HealthFailing
	}
}

// ComputeScore turns a flat list of newest-first invocations into a
// SkillScore (counts, success rate, rolling 7d trend, health). It
// does not touch the database — it is pure so it can be exercised
// directly from tests.
func ComputeScore(skillName string, invocations []Invocation, now time.Time) SkillScore {
	score := SkillScore{
		SkillName:   skillName,
		Invocations: len(invocations),
		Health:      ClassifyHealth(invocations),
	}
	if len(invocations) == 0 {
		return score
	}

	var totalDuration int64
	var successes int
	var last7Count, last7Success int
	var prior7Count, prior7Success int

	now = now.UTC()
	last7Cutoff := now.Add(-7 * 24 * time.Hour)
	prior7Cutoff := now.Add(-14 * 24 * time.Hour)

	score.LastInvokedAt = invocations[0].Timestamp

	for _, inv := range invocations {
		totalDuration += inv.DurationMs
		if inv.Success {
			successes++
		}
		switch {
		case inv.Timestamp.After(last7Cutoff):
			last7Count++
			if inv.Success {
				last7Success++
			}
		case inv.Timestamp.After(prior7Cutoff):
			prior7Count++
			if inv.Success {
				prior7Success++
			}
		}
	}

	score.SuccessRate = pct(successes, len(invocations))
	score.AvgDurationMs = float64(totalDuration) / float64(len(invocations))
	score.Last7dCount = last7Count
	score.Last7dRate = pct(last7Success, last7Count)
	score.Prior7dCount = prior7Count
	score.Prior7dRate = pct(prior7Success, prior7Count)

	// first_seen_failing: walk newest-first looking for the
	// earliest point at which the trailing-20 window was failing.
	score.FirstSeenFailing = firstSeenFailing(invocations)
	return score
}

// firstSeenFailing returns the earliest invocation timestamp at
// which the trailing-20 health window first crossed into "failing".
// Zero time means the skill is not currently failing (or there
// aren't enough rows to tell).
func firstSeenFailing(invocations []Invocation) time.Time {
	if ClassifyHealth(invocations) != HealthFailing {
		return time.Time{}
	}
	// Walk from oldest forward. For each suffix newest-first window
	// of size <=20, check if it's failing. The earliest timestamp
	// in the first failing suffix is "first seen failing".
	// invocations is newest-first; reverse for easier reasoning.
	chrono := make([]Invocation, len(invocations))
	for i, inv := range invocations {
		chrono[len(invocations)-1-i] = inv
	}
	for i := 0; i < len(chrono); i++ {
		// Build trailing-20 newest-first window ending at index i.
		end := i + 1
		start := end - healthWindow
		if start < 0 {
			start = 0
		}
		window := make([]Invocation, end-start)
		for j := start; j < end; j++ {
			window[end-1-j] = chrono[j]
		}
		if ClassifyHealth(window) == HealthFailing {
			return chrono[start].Timestamp
		}
	}
	// Fallback: just use the oldest invocation in the failing
	// classification (shouldn't usually reach here if ClassifyHealth
	// said failing).
	return invocations[len(invocations)-1].Timestamp
}

// ComputeAllScores aggregates scores for every skill that has at
// least one invocation in the store.
func ComputeAllScores(store *Store, now time.Time) ([]SkillScore, error) {
	names, err := store.ListSkillNames()
	if err != nil {
		return nil, err
	}
	scores := make([]SkillScore, 0, len(names))
	for _, name := range names {
		invs, err := store.LoadInvocations(name, 0)
		if err != nil {
			return nil, err
		}
		scores = append(scores, ComputeScore(name, invs, now))
	}
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].SkillName < scores[j].SkillName
	})
	return scores, nil
}

// ComputePruneSuggestions returns the list of skills classified as
// "failing" AND whose first_seen_failing timestamp is older than
// pruneFailingFor (7 days). It does not actually prune anything —
// the curator is observation-only in v0.
func ComputePruneSuggestions(store *Store, now time.Time) ([]PruneSuggestion, error) {
	scores, err := ComputeAllScores(store, now)
	if err != nil {
		return nil, err
	}
	var out []PruneSuggestion
	cutoff := now.Add(-pruneFailingFor)
	for _, s := range scores {
		if s.Health != HealthFailing {
			continue
		}
		if s.FirstSeenFailing.IsZero() || !s.FirstSeenFailing.Before(cutoff) {
			continue
		}
		out = append(out, PruneSuggestion{
			SkillName:        s.SkillName,
			FirstSeenFailing: s.FirstSeenFailing,
			FailingForDays:   now.Sub(s.FirstSeenFailing).Hours() / 24,
			SuccessRate:      s.SuccessRate,
			Invocations:      s.Invocations,
		})
	}
	return out, nil
}

func pct(num, denom int) float64 {
	if denom == 0 {
		return 0
	}
	return float64(num) / float64(denom) * 100
}
