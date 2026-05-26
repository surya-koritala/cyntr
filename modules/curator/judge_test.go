package curator

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// stubProvider is a minimal agent.ModelProvider implementation that
// returns a canned response and counts invocations. We don't reuse
// providers.NewMock here because we need to vary the reply per call
// and assert payload contents — keeping the stub local also avoids
// a (currently absent) circular import between curator and tools.
type stubProvider struct {
	calls    int32
	reply    string
	captured []agent.Message
}

func (s *stubProvider) Name() string { return "stub" }

func (s *stubProvider) Chat(ctx context.Context, msgs []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	atomic.AddInt32(&s.calls, 1)
	s.captured = append(s.captured, msgs...)
	return agent.Message{Role: agent.RoleAssistant, Content: s.reply}, nil
}

func TestJudgeParsesCleanJSON(t *testing.T) {
	p := &stubProvider{reply: `{"score": 0.9, "reason": "Resolved the request precisely.", "verdict": "good"}`}
	j := NewJudge(p, "test-model")

	res, err := j.ScoreInvocation(context.Background(), InvocationContext{
		SkillName:     "code-review",
		UserMessage:   "Review this PR.",
		AgentResponse: "PR looks good.",
		Success:       true,
	})
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if res.Score != 0.9 || res.Verdict != VerdictGood {
		t.Fatalf("unexpected result %+v", res)
	}
	if atomic.LoadInt32(&p.calls) != 1 {
		t.Fatalf("expected 1 provider call, got %d", p.calls)
	}
	// System prompt and the user message should both be there.
	if len(p.captured) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(p.captured))
	}
	if p.captured[0].Role != agent.RoleSystem {
		t.Fatalf("first message should be system, got %v", p.captured[0].Role)
	}
}

func TestJudgeStripsCodeFences(t *testing.T) {
	p := &stubProvider{reply: "```json\n{\"score\": 0.4, \"reason\": \"Partially correct.\", \"verdict\": \"acceptable\"}\n```"}
	j := NewJudge(p, "test-model")
	res, err := j.ScoreInvocation(context.Background(), InvocationContext{SkillName: "x"})
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if res.Score != 0.4 || res.Verdict != VerdictAcceptable {
		t.Fatalf("unexpected: %+v", res)
	}
}

func TestJudgeDerivesVerdictFromScoreOnGarbage(t *testing.T) {
	p := &stubProvider{reply: `here you go: {"score": 0.2, "reason": "bad", "verdict": "definitely not"}`}
	j := NewJudge(p, "test-model")
	res, err := j.ScoreInvocation(context.Background(), InvocationContext{SkillName: "x"})
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if res.Verdict != VerdictPoor {
		t.Fatalf("expected poor verdict from low score, got %q", res.Verdict)
	}
}

// TestJudgeAndPersistFlowsScoreToStore verifies the spec's "updates
// the llm_judge_score column on the invocation row".
func TestJudgeAndPersistFlowsScoreToStore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "c.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()

	id, err := store.RecordID(Invocation{SkillName: "judged", Success: true, DurationMs: 1})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	p := &stubProvider{reply: `{"score": 0.75, "reason": "ok", "verdict": "acceptable"}`}
	j := NewJudge(p, "m")
	if _, err := j.JudgeAndPersist(context.Background(), store, InvocationContext{
		SkillName:    "judged",
		InvocationID: id,
		Success:      true,
	}); err != nil {
		t.Fatalf("judge: %v", err)
	}

	invs, _ := store.LoadInvocations("judged", 0)
	if len(invs) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(invs))
	}
	if invs[0].LLMJudgeScore == nil || *invs[0].LLMJudgeScore != 0.75 {
		t.Fatalf("expected score 0.75 in store, got %+v", invs[0].LLMJudgeScore)
	}
}

// TestJudgeShouldJudgeRateLimit verifies the spec's "max 1 judge
// call per skill per 10 invocations" rule.
func TestJudgeShouldJudgeRateLimit(t *testing.T) {
	dir := t.TempDir()
	store, _ := NewStore(filepath.Join(dir, "c.db"))
	defer store.Close()

	p := &stubProvider{reply: `{"score": 0.9, "reason": "ok", "verdict": "good"}`}
	j := NewJudge(p, "m")

	// 0 invocations -> no.
	ok, err := j.ShouldJudge(store, "skl")
	if err != nil {
		t.Fatalf("should: %v", err)
	}
	if ok {
		t.Fatal("expected no-judge at 0 invocations")
	}

	// 1 invocation, never judged -> yes.
	id, _ := store.RecordID(Invocation{SkillName: "skl", Success: true})
	ok, _ = j.ShouldJudge(store, "skl")
	if !ok {
		t.Fatal("expected judge at first invocation")
	}

	// After judging, the same skill should NOT be re-judgable until
	// 10 more invocations.
	if _, err := j.JudgeAndPersist(context.Background(), store, InvocationContext{SkillName: "skl", InvocationID: id}); err != nil {
		t.Fatalf("judge: %v", err)
	}
	ok, _ = j.ShouldJudge(store, "skl")
	if ok {
		t.Fatal("expected rate-limit to block a re-judge after one invocation")
	}

	// Add 9 more invocations -> total - last = 9 (still under 10).
	for i := 0; i < 9; i++ {
		store.RecordID(Invocation{SkillName: "skl", Success: true})
	}
	ok, _ = j.ShouldJudge(store, "skl")
	if ok {
		t.Fatal("expected rate-limit to still block at 9 invocations past watermark")
	}

	// One more — now 10 invocations have passed since the last
	// judgment, so the window has elapsed.
	store.RecordID(Invocation{SkillName: "skl", Success: true})
	ok, _ = j.ShouldJudge(store, "skl")
	if !ok {
		t.Fatal("expected judge to be allowed after 10 invocations since last judgment")
	}
}
