package curator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// JudgeRateWindow is the number of invocations that must accumulate
// between LLM judge calls for the same skill. Without rate-limiting
// the judge would burn tokens on every recorded invocation; the spec
// is "max one judge call per skill per 10 invocations".
const JudgeRateWindow = 10

// judgeSystemPrompt is intentionally compact — the cheaper the
// judge model, the more important the prompt is at being directive
// rather than chatty. We ask for raw JSON to keep parsing simple.
const judgeSystemPrompt = `You're judging whether an AI skill performed well. Given the user's request and the agent's response, rate the response 0-1 where 0=completely wrong, 1=perfect. Be strict but fair.

Output ONLY a single JSON object on one line, no prose, no markdown fences:
{"score": <0-1 float>, "reason": "<one short sentence>", "verdict": "<good|acceptable|poor>"}`

// Judge wraps an LLM provider with rate-limited, JSON-parsing logic
// for scoring skill invocations. The provider is invoked via the
// existing agent.ModelProvider interface so the judge inherits all
// of the platform's provider abstractions (no new deps).
type Judge struct {
	provider agent.ModelProvider
	model    string

	// In-memory rate-limit bookkeeping. We *also* persist the last
	// judged invocation count via Store.CountJudged so a restart
	// doesn't reset the limiter; the in-memory map is a fast path.
	mu               sync.Mutex
	lastJudgedCount  map[string]int // skill_name -> CountInvocations() at last judgment
}

// NewJudge constructs a Judge. model is the model identifier passed
// through to the provider (e.g. "claude-haiku-4-5" or "gpt-4o-mini")
// — the judge does not interpret it.
func NewJudge(provider agent.ModelProvider, model string) *Judge {
	return &Judge{
		provider:        provider,
		model:           model,
		lastJudgedCount: make(map[string]int),
	}
}

// ScoreInvocation prompts the provider to grade a single invocation.
// It does not enforce rate limiting itself — callers (the curator
// module) decide when to invoke it. This keeps the unit testable and
// the rate logic visible to readers.
func (j *Judge) ScoreInvocation(ctx context.Context, inv InvocationContext) (*JudgeResult, error) {
	if j.provider == nil {
		return nil, fmt.Errorf("judge: no provider configured")
	}
	user := buildJudgeUserMessage(inv)
	msgs := []agent.Message{
		{Role: agent.RoleSystem, Content: judgeSystemPrompt},
		{Role: agent.RoleUser, Content: user},
	}
	resp, err := j.provider.Chat(ctx, msgs, nil)
	if err != nil {
		return nil, fmt.Errorf("judge: provider error: %w", err)
	}
	result, err := parseJudgeResponse(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("judge: parse response: %w (raw: %q)", err, resp.Content)
	}
	return result, nil
}

// ShouldJudge implements the spec's rate limit: at most one judge
// call per skill per JudgeRateWindow invocations. The store is the
// source of truth (so restarts don't reset the limiter); the in-mem
// map is a tiny optimisation that avoids hitting the DB on every
// invocation.
func (j *Judge) ShouldJudge(store *Store, skillName string) (bool, error) {
	total, err := store.CountInvocations(skillName)
	if err != nil {
		return false, err
	}
	if total == 0 {
		return false, nil
	}

	j.mu.Lock()
	last, ok := j.lastJudgedCount[skillName]
	j.mu.Unlock()
	if !ok {
		// First time we've seen this skill in this process — consult
		// the DB to recover any pre-restart judgment count.
		judged, err := store.CountJudged(skillName)
		if err != nil {
			return false, err
		}
		// If we've never judged it, allow now. Otherwise treat the
		// count at "last judgment" as approximately (total - 0) when
		// judged>0; this is a heuristic, but worst case we judge one
		// extra time after restart, which is fine.
		if judged == 0 {
			return true, nil
		}
		// Recover by storing the current total as the watermark; the
		// next gate will use it cleanly.
		j.mu.Lock()
		j.lastJudgedCount[skillName] = total - JudgeRateWindow
		last = j.lastJudgedCount[skillName]
		j.mu.Unlock()
	}
	return total-last >= JudgeRateWindow, nil
}

// markJudged is called after a successful judgment so the rate
// limiter advances the watermark for the next call.
func (j *Judge) markJudged(store *Store, skillName string) {
	total, err := store.CountInvocations(skillName)
	if err != nil {
		return
	}
	j.mu.Lock()
	j.lastJudgedCount[skillName] = total
	j.mu.Unlock()
}

// JudgeAndPersist runs ScoreInvocation, then writes the score back
// to the invocation row if InvocationID is set, and advances the
// rate limiter. This is the convenience wrapper the curator module
// uses internally; tests can drive either the inner or outer call.
func (j *Judge) JudgeAndPersist(ctx context.Context, store *Store, inv InvocationContext) (*JudgeResult, error) {
	res, err := j.ScoreInvocation(ctx, inv)
	if err != nil {
		return nil, err
	}
	if inv.InvocationID > 0 {
		if err := store.SetJudgeScore(inv.InvocationID, res.Score); err != nil {
			return res, fmt.Errorf("judge: persist score: %w", err)
		}
	}
	j.markJudged(store, inv.SkillName)
	return res, nil
}

// buildJudgeUserMessage formats an InvocationContext into a single
// user-turn payload. Keeping it as plain text (not JSON) keeps the
// prompt cheap and gives the model a normal-looking conversation.
func buildJudgeUserMessage(inv InvocationContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Skill invoked: %s\n", inv.SkillName)
	if inv.UserMessage != "" {
		fmt.Fprintf(&b, "\nUser request:\n%s\n", inv.UserMessage)
	}
	if inv.AgentResponse != "" {
		fmt.Fprintf(&b, "\nAgent response:\n%s\n", inv.AgentResponse)
	}
	if len(inv.ToolsUsed) > 0 {
		fmt.Fprintf(&b, "\nTools used: %s\n", strings.Join(inv.ToolsUsed, ", "))
	}
	fmt.Fprintf(&b, "\nSuccess (per runtime): %t", inv.Success)
	if inv.Error != "" {
		fmt.Fprintf(&b, "\nRuntime error: %s", inv.Error)
	}
	return b.String()
}

// parseJudgeResponse pulls a JSON object out of the model's reply
// and normalises the fields. Some providers wrap JSON in code fences
// or add a sentence of prose despite the instruction; we recover by
// scanning for the first { ... } block.
func parseJudgeResponse(raw string) (*JudgeResult, error) {
	body := strings.TrimSpace(raw)
	// Strip markdown code fences if present.
	if strings.HasPrefix(body, "```") {
		body = strings.TrimPrefix(body, "```json")
		body = strings.TrimPrefix(body, "```")
		body = strings.TrimSuffix(body, "```")
		body = strings.TrimSpace(body)
	}
	// Find the first JSON object substring as a fallback.
	if !strings.HasPrefix(body, "{") {
		start := strings.Index(body, "{")
		end := strings.LastIndex(body, "}")
		if start < 0 || end < 0 || end <= start {
			return nil, fmt.Errorf("no JSON object found")
		}
		body = body[start : end+1]
	}

	var raw2 struct {
		Score   float64 `json:"score"`
		Reason  string  `json:"reason"`
		Verdict string  `json:"verdict"`
	}
	if err := json.Unmarshal([]byte(body), &raw2); err != nil {
		return nil, err
	}
	if raw2.Score < 0 {
		raw2.Score = 0
	}
	if raw2.Score > 1 {
		raw2.Score = 1
	}
	verdict := strings.ToLower(strings.TrimSpace(raw2.Verdict))
	switch verdict {
	case VerdictGood, VerdictAcceptable, VerdictPoor:
		// ok
	default:
		// Derive from score when the model gave us garbage.
		switch {
		case raw2.Score >= 0.8:
			verdict = VerdictGood
		case raw2.Score >= 0.5:
			verdict = VerdictAcceptable
		default:
			verdict = VerdictPoor
		}
	}
	return &JudgeResult{
		Score:   raw2.Score,
		Reason:  strings.TrimSpace(raw2.Reason),
		Verdict: verdict,
	}, nil
}
