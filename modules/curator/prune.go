package curator

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// TopicSkillDisabled is the IPC topic published when the auto-prune
// loop disables a skill. notify / audit / dashboard modules can
// subscribe to surface the action.
const TopicSkillDisabled = "skill.disabled"

// PruneSkillDisabler is the surface the curator needs to actually
// turn a skill off. The skill.Registry implements it directly;
// tests can inject a fake. Keeping it as an interface here avoids
// an import cycle between curator and skill packages — the curator
// module is wired with the registry at boot.
type PruneSkillDisabler interface {
	Disable(name, reason string) error
}

// SetSkillDisabler wires the registry that the prune loop calls
// when it decides to disable a skill. If unset, PruneFailingSkills
// still produces a report but never flips the disabled bit (the
// suggestion list, essentially). main.go is expected to set this
// during boot; tests inject directly.
func (m *Module) SetSkillDisabler(d PruneSkillDisabler) {
	m.disabler = d
}

// PruneFailingSkills runs one pass of the auto-prune loop:
//
//   1. Ask the existing curator.suggest_prune logic for skills that
//      have been classified "failing" for >7 days.
//   2. For each, pull up to 5 recent failure samples to include in
//      the audit trail.
//   3. Mark the skill disabled in the registry (reversible).
//   4. Publish a skill.disabled IPC event so notify/audit pick it up.
//   5. Return a structured report so the API caller can show it.
//
// The operation is idempotent: re-running it for an already-disabled
// skill produces a report entry with Disabled=false and a reason of
// "already disabled" so cron retries don't double-fire alerts.
func (m *Module) PruneFailingSkills(ctx context.Context) (*PruneReport, error) {
	if m.store == nil {
		return nil, fmt.Errorf("curator: prune called before init")
	}
	now := m.now()
	suggestions, err := ComputePruneSuggestions(m.store, now)
	if err != nil {
		return nil, err
	}

	report := &PruneReport{RanAt: now, Entries: []PruneReportEntry{}}
	for _, s := range suggestions {
		samples, _ := m.store.RecentFailureSamples(s.SkillName, 5)
		reason := fmt.Sprintf(
			"failing for %.1f days (%.0f%% success rate over %d invocations)",
			s.FailingForDays, s.SuccessRate, s.Invocations,
		)
		entry := PruneReportEntry{
			Skill:   s.SkillName,
			Reason:  reason,
			Samples: samples,
		}

		if m.disabler == nil {
			// No registry wired — report only. main.go injects in prod.
			entry.Disabled = false
			report.Entries = append(report.Entries, entry)
			continue
		}

		if err := m.disabler.Disable(s.SkillName, reason); err != nil {
			// Idempotency: if it's already disabled, treat as success-
			// but-no-op so cron doesn't keep alerting.
			if isAlreadyDisabled(err) {
				entry.Disabled = false
				entry.Reason = "already disabled"
				report.Entries = append(report.Entries, entry)
				continue
			}
			entry.Disabled = false
			entry.Reason = fmt.Sprintf("disable failed: %v", err)
			report.Entries = append(report.Entries, entry)
			continue
		}
		entry.Disabled = true
		report.Entries = append(report.Entries, entry)

		// Fire a skill.disabled event for notify / audit consumers.
		if m.bus != nil {
			_ = m.bus.Publish(ipc.Message{
				Source: ModuleName,
				Type:   ipc.MessageTypeEvent,
				Topic:  TopicSkillDisabled,
				Payload: SkillDisabledEvent{
					Skill:   s.SkillName,
					Reason:  reason,
					Samples: samples,
					At:      now,
				},
			})
		}
	}
	return report, nil
}

// isAlreadyDisabled is a sentinel string check; we don't introduce a
// typed error for it because PruneSkillDisabler is implementation-
// agnostic on purpose. The skill registry returns an error whose
// message contains "already disabled" when re-disabling a skill.
func isAlreadyDisabled(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "already disabled")
}

// pruneCadenceFromEnv returns the configured tick interval for the
// background prune loop. Default is 24h; operators can shorten it
// via CYNTR_CURATOR_PRUNE_INTERVAL (Go duration string) for tests.
// We don't use the heavyweight scheduler module for this — it's a
// single internal job that should not need a job-store row.
func pruneCadenceFromEnv() time.Duration {
	v := os.Getenv("CYNTR_CURATOR_PRUNE_INTERVAL")
	if v == "" {
		return 24 * time.Hour
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 24 * time.Hour
	}
	return d
}
