package curator

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func TestModuleImplementsKernelModule(t *testing.T) {
	var _ kernel.Module = (*Module)(nil)
}

// TestModuleRecordViaIPC verifies the end-to-end fire-and-forget
// record path: publish curator.record → row appears in the store.
func TestModuleRecordViaIPC(t *testing.T) {
	dir := t.TempDir()
	bus := ipc.NewBus()
	defer bus.Close()

	mod := New(filepath.Join(dir, "curator.db"))
	ctx := context.Background()
	if err := mod.Init(ctx, &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := mod.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer mod.Stop(ctx)

	bus.Publish(ipc.Message{
		Source: "skill_router", Type: ipc.MessageTypeEvent, Topic: TopicRecord,
		Payload: Invocation{
			SkillName: "code-review", Tenant: "acme", Agent: "reviewer",
			Success: true, DurationMs: 42, Timestamp: time.Now().UTC(),
		},
	})

	// Subscribe handler runs in a goroutine — wait briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := mod.Store().LoadInvocations("code-review", 0)
		if len(got) == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("invocation never persisted")
}

// TestModuleScoresViaIPC verifies the request/response curator.scores
// IPC topic, including the skill_name filter.
func TestModuleScoresViaIPC(t *testing.T) {
	dir := t.TempDir()
	bus := ipc.NewBus()
	defer bus.Close()

	mod := New(filepath.Join(dir, "curator.db"))
	ctx := context.Background()
	if err := mod.Init(ctx, &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := mod.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer mod.Stop(ctx)

	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		mod.Store().Record(Invocation{
			SkillName: "skill-A", Tenant: "t", Agent: "g",
			Success: true, DurationMs: 10, Timestamp: now,
		})
	}
	for i := 0; i < 5; i++ {
		mod.Store().Record(Invocation{
			SkillName: "skill-B", Tenant: "t", Agent: "g",
			Success: false, DurationMs: 20, Timestamp: now,
		})
	}

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Unfiltered → both skills.
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "api", Target: ModuleName, Topic: TopicScores,
	})
	if err != nil {
		t.Fatalf("scores: %v", err)
	}
	scores, ok := resp.Payload.([]SkillScore)
	if !ok {
		t.Fatalf("expected []SkillScore, got %T", resp.Payload)
	}
	if len(scores) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(scores))
	}

	// Filtered → just skill-A.
	resp, err = bus.Request(reqCtx, ipc.Message{
		Source: "api", Target: ModuleName, Topic: TopicScores,
		Payload: ScoresFilter{SkillName: "skill-A"},
	})
	if err != nil {
		t.Fatalf("scores filter: %v", err)
	}
	filtered, ok := resp.Payload.([]SkillScore)
	if !ok {
		t.Fatalf("expected []SkillScore, got %T", resp.Payload)
	}
	if len(filtered) != 1 || filtered[0].SkillName != "skill-A" {
		t.Fatalf("expected just skill-A, got %+v", filtered)
	}
	if filtered[0].SuccessRate != 100.0 {
		t.Fatalf("expected 100%% success for skill-A, got %f", filtered[0].SuccessRate)
	}
}

// TestModuleJudgeViaIPC verifies the new curator.judge IPC topic
// drives the judge end-to-end with a stubbed provider.
func TestModuleJudgeViaIPC(t *testing.T) {
	dir := t.TempDir()
	bus := ipc.NewBus()
	defer bus.Close()

	mod := New(filepath.Join(dir, "curator.db"))
	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)

	// Seed one invocation so the judge has something to score.
	id, err := mod.Store().RecordID(Invocation{SkillName: "skl", Success: true, DurationMs: 5})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	provider := &stubProvider{reply: `{"score": 0.6, "reason": "ok", "verdict": "acceptable"}`}
	mod.SetJudge(NewJudge(provider, "test-model"))

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "api", Target: ModuleName, Topic: TopicJudge,
		Payload: InvocationContext{
			SkillName:    "skl",
			InvocationID: id,
			Success:      true,
		},
	})
	if err != nil {
		t.Fatalf("judge ipc: %v", err)
	}
	result, ok := resp.Payload.(*JudgeResult)
	if !ok {
		t.Fatalf("expected *JudgeResult, got %T", resp.Payload)
	}
	if result.Verdict != VerdictAcceptable {
		t.Fatalf("expected acceptable, got %q", result.Verdict)
	}

	// Score should also have flowed to the store.
	invs, _ := mod.Store().LoadInvocations("skl", 0)
	if len(invs) != 1 || invs[0].LLMJudgeScore == nil || *invs[0].LLMJudgeScore != 0.6 {
		t.Fatalf("expected score 0.6 persisted, got %+v", invs)
	}
}

// TestModuleJudgeUnavailableWhenNotWired confirms the safety
// rail — calling curator.judge with no provider returns an error
// rather than a panic.
func TestModuleJudgeUnavailableWhenNotWired(t *testing.T) {
	dir := t.TempDir()
	bus := ipc.NewBus()
	defer bus.Close()

	mod := New(filepath.Join(dir, "curator.db"))
	mod.Init(context.Background(), &kernel.Services{Bus: bus})
	mod.Start(context.Background())
	defer mod.Stop(context.Background())

	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := bus.Request(reqCtx, ipc.Message{
		Source: "api", Target: ModuleName, Topic: TopicJudge,
		Payload: InvocationContext{SkillName: "x"},
	})
	if err == nil {
		t.Fatal("expected error when no judge wired")
	}
}

func TestModuleSuggestPruneViaIPC(t *testing.T) {
	dir := t.TempDir()
	bus := ipc.NewBus()
	defer bus.Close()

	mod := New(filepath.Join(dir, "curator.db"))
	// Pin "now" so the test is deterministic.
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	mod.now = func() time.Time { return now }

	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)

	// 20 failures stretched across the past 10 days.
	for i := 0; i < 20; i++ {
		mod.Store().Record(Invocation{
			SkillName: "doomed", Tenant: "t", Agent: "g",
			Success: false, DurationMs: 100,
			Timestamp: now.Add(-time.Duration(10*24-i*12) * time.Hour),
		})
	}

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "api", Target: ModuleName, Topic: TopicSuggestPrune,
	})
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	suggestions, ok := resp.Payload.([]PruneSuggestion)
	if !ok {
		t.Fatalf("expected []PruneSuggestion, got %T", resp.Payload)
	}
	if len(suggestions) != 1 || suggestions[0].SkillName != "doomed" {
		t.Fatalf("expected doomed, got %+v", suggestions)
	}
}
