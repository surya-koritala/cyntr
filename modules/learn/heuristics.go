package learn

import "github.com/cyntr-dev/cyntr/modules/agent"

// shouldReflect decides whether a completed turn is worth an (LLM-priced)
// reflection. The current signal is task complexity: a turn that used at
// least minToolCalls tools did real work worth learning from. Trivial
// single-shot answers are skipped.
func shouldReflect(rec agent.TurnRecord, minToolCalls int) bool {
	if rec.Tenant == "" || rec.Agent == "" {
		return false
	}
	// Don't reflect on subagent-internal turns — the parent's turn already
	// captures the overall task (#47).
	if rec.Subagent {
		return false
	}
	if minToolCalls <= 0 {
		minToolCalls = DefaultMinToolCalls
	}
	return rec.ToolCalls >= minToolCalls
}
