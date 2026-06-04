package curator

import (
	"context"
	"fmt"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// improveSystemPrompt instructs the model to rewrite a failing skill's
// instructions given recent failures. It returns the SKILL.md body only.
const improveSystemPrompt = `You are improving an AI agent skill that has been failing. You are given its current instructions and samples of recent failed runs. Rewrite the instructions so the skill handles those failures better.

Current instructions:
---
%s
---

Recent failures:
---
%s
---

Return ONLY the improved SKILL.md body (markdown). Keep what works, fix what caused the failures, stay concise. Do not include secrets.`

// FetchInstructionsFunc returns the current SKILL.md body for a skill. Wired
// to the skill.get IPC in main.go; injected so the curator stays free of a
// hard dependency on the skill package (mirroring consolidate.go).
type FetchInstructionsFunc func(name string) (string, error)

// ProposeFunc submits an improved skill as an approval-gated candidate. Wired
// to the skill.propose IPC in main.go. The improvement is NEVER applied
// directly — it becomes a pending candidate the operator (or safe-capability
// policy) approves, and approval replaces the live skill while keeping the
// prior version for rollback.
type ProposeFunc func(name, description, instructions string) error

// Improver turns a failing skill's recent failures into a proposed, improved
// version. It never mutates a live skill — it only proposes.
type Improver struct {
	provider agent.ModelProvider
	model    string
	fetch    FetchInstructionsFunc
	propose  ProposeFunc
	// minFailures is how many failure samples must exist before we spend an
	// LLM call trying to improve a skill.
	minFailures int
}

// NewImprover constructs an Improver. fetch and propose are required.
func NewImprover(provider agent.ModelProvider, model string, fetch FetchInstructionsFunc, propose ProposeFunc) *Improver {
	return &Improver{provider: provider, model: model, fetch: fetch, propose: propose, minFailures: 1}
}

// Improve generates and proposes an improved version of one skill. Returns
// (true, nil) when a proposal was raised, (false, nil) when there was nothing
// to learn from (no failures), and an error on a hard failure.
func (im *Improver) Improve(ctx context.Context, store *Store, skillName string) (bool, error) {
	if im.provider == nil || im.fetch == nil || im.propose == nil {
		return false, fmt.Errorf("curator: improver not fully configured")
	}
	samples, err := store.RecentFailureSamples(skillName, 5)
	if err != nil {
		return false, err
	}
	if len(samples) < im.minFailures {
		return false, nil // nothing to learn from yet
	}
	current, err := im.fetch(skillName)
	if err != nil {
		return false, fmt.Errorf("curator: fetch instructions for %q: %w", skillName, err)
	}

	prompt := fmt.Sprintf(improveSystemPrompt, defaultIfBlank(current, "(none)"), strings.Join(samples, "\n---\n"))
	resp, err := im.provider.Chat(ctx, []agent.Message{{Role: agent.RoleUser, Content: prompt}}, nil)
	if err != nil {
		return false, fmt.Errorf("curator: improve %q: %w", skillName, err)
	}
	improved := strings.TrimSpace(stripFence(resp.Content))
	if improved == "" {
		return false, nil
	}
	if err := im.propose(skillName, "auto-improved by curator from recent failures", improved); err != nil {
		return false, fmt.Errorf("curator: propose improvement for %q: %w", skillName, err)
	}
	return true, nil
}

func defaultIfBlank(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
