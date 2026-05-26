package curator

import (
	"context"
	"fmt"
	"sort"
)

// ConsolidationMinInvocations is the spec's "both have >50
// invocations" gate. Without it the curator would flood the operator
// with overlap warnings for half-installed catalog skills.
const ConsolidationMinInvocations = 50

// ConsolidationJaccardThreshold is the spec's "Jaccard >= 0.6" gate
// on overlapping tool surfaces. Higher = stricter = fewer pairs.
const ConsolidationJaccardThreshold = 0.6

// ConsolidationSkillSnapshot is a minimal view of a skill that the
// consolidator needs: its tools, invocation count, and name. Wiring
// the skill registry through an interface keeps this module free of
// a skill package import (and free of a test-time dependency on the
// full skill loader).
type ConsolidationSkillSnapshot struct {
	Name        string
	Tools       []string
	Invocations int
}

// ConsolidationSnapshotter is the surface the consolidator pulls
// from. The curator module wires the skill registry as the prod
// implementation; tests inject a slice directly.
type ConsolidationSnapshotter interface {
	SkillsForConsolidation() []ConsolidationSkillSnapshot
}

// SetSnapshotter wires the source of truth for skill metadata used
// by SuggestConsolidation. If unset, the report is empty (graceful
// degradation rather than a hard error — the v0 curator API still
// works).
func (m *Module) SetSnapshotter(s ConsolidationSnapshotter) {
	m.snapshotter = s
}

// SuggestConsolidation walks every pair of currently-installed
// skills and emits a suggestion when their tool surfaces overlap by
// at least ConsolidationJaccardThreshold AND both pass the
// invocation-count floor. It does *not* act; it's read-only signal.
func (m *Module) SuggestConsolidation(ctx context.Context) (*ConsolidationReport, error) {
	report := &ConsolidationReport{
		GeneratedAt: m.now(),
		Suggestions: []ConsolidationSuggestion{},
	}
	if m.snapshotter == nil {
		return report, nil
	}
	skills := m.snapshotter.SkillsForConsolidation()

	// Filter to skills that pass the invocation floor — pairs are
	// only considered if BOTH are above the floor, so we can prune
	// early.
	eligible := make([]ConsolidationSkillSnapshot, 0, len(skills))
	for _, s := range skills {
		if s.Invocations >= ConsolidationMinInvocations && len(s.Tools) > 0 {
			eligible = append(eligible, s)
		}
	}
	// Stable order so reports are reproducible.
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].Name < eligible[j].Name })

	for i := 0; i < len(eligible); i++ {
		for j := i + 1; j < len(eligible); j++ {
			a := eligible[i]
			b := eligible[j]
			shared, jaccard := jaccardOverlap(a.Tools, b.Tools)
			if jaccard < ConsolidationJaccardThreshold {
				continue
			}
			report.Suggestions = append(report.Suggestions, ConsolidationSuggestion{
				SkillA:       a.Name,
				SkillB:       b.Name,
				SharedTools:  shared,
				Jaccard:      jaccard,
				InvocationsA: a.Invocations,
				InvocationsB: b.Invocations,
				Note: fmt.Sprintf(
					"Skills %s and %s overlap on tools {%s}; consider merging.",
					a.Name, b.Name, strJoin(shared, ", "),
				),
			})
		}
	}
	return report, nil
}

// jaccardOverlap returns the sorted intersection plus the Jaccard
// similarity |A∩B| / |A∪B|. Treats input as unordered sets — dupes
// in the input are squashed.
func jaccardOverlap(a, b []string) ([]string, float64) {
	setA := toSet(a)
	setB := toSet(b)
	if len(setA) == 0 || len(setB) == 0 {
		return nil, 0
	}
	shared := []string{}
	for k := range setA {
		if _, ok := setB[k]; ok {
			shared = append(shared, k)
		}
	}
	union := len(setA) + len(setB) - len(shared)
	if union == 0 {
		return nil, 0
	}
	sort.Strings(shared)
	return shared, float64(len(shared)) / float64(union)
}

func toSet(xs []string) map[string]struct{} {
	m := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		if x == "" {
			continue
		}
		m[x] = struct{}{}
	}
	return m
}

// strJoin is a tiny helper that avoids pulling strings just for one
// call elsewhere in this file. (consolidate.go would otherwise need
// `strings` only for Join.)
func strJoin(xs []string, sep string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	out := xs[0]
	for _, x := range xs[1:] {
		out += sep + x
	}
	return out
}
