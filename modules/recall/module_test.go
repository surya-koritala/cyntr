package recall

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/jobs"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func startModule(t *testing.T, opts ...Option) (*Module, *ipc.Bus) {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "recall.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	m := New(store, opts...)
	bus := ipc.NewBus()
	t.Cleanup(bus.Close)
	if err := m.Init(context.Background(), &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return m, bus
}

func turn(tenant, user, session, msg, resp string) ipc.Message {
	return ipc.Message{
		Topic: agent.TopicTurnCompleted, Type: ipc.MessageTypeEvent,
		Payload: agent.TurnRecord{
			Tenant: tenant, User: user, Session: session,
			UserMessage: msg, Response: resp, StartedAt: time.Now().UTC(), Outcome: "ok",
		},
	}
}

func TestModuleIndexesTurnAndSearchesOverBus(t *testing.T) {
	m, bus := startModule(t)

	// Simulate a completed turn (called directly for determinism — the real
	// path is a fire-and-forget Subscribe on this same topic).
	if _, err := m.handleTurnCompleted(turn("acme", "jane", "s1", "what's our q3 budget plan", "the q3 budget is finalized")); err != nil {
		t.Fatalf("handleTurnCompleted: %v", err)
	}

	// Search over the IPC topic the recall_search tool uses.
	resp, err := bus.Request(context.Background(), ipc.Message{
		Source: "test", Target: "recall", Topic: TopicSearch,
		Payload: SearchRequest{Tenant: "acme", User: "jane", Query: "budget", Limit: 5},
	})
	if err != nil {
		t.Fatalf("recall.search: %v", err)
	}
	res, ok := resp.Payload.(SearchResult)
	if !ok {
		t.Fatalf("unexpected payload %T", resp.Payload)
	}
	if len(res.Snippets) == 0 {
		t.Fatal("expected at least one snippet for 'budget' after indexing a turn")
	}

	// A different tenant must not see jane@acme's history.
	resp, _ = bus.Request(context.Background(), ipc.Message{
		Source: "test", Target: "recall", Topic: TopicSearch,
		Payload: SearchRequest{Tenant: "globex", User: "jane", Query: "budget", Limit: 5},
	})
	if r := resp.Payload.(SearchResult); len(r.Snippets) != 0 {
		t.Fatalf("cross-tenant leak: %d snippets", len(r.Snippets))
	}
}

func TestModuleSummarizeJobFlow(t *testing.T) {
	queue, err := jobs.NewQueue(filepath.Join(t.TempDir(), "jobs.db"), jobs.WithBackoff(func(int) time.Duration { return 0 }))
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	t.Cleanup(func() { queue.Close() })

	var summarizeCalls atomic.Int32
	fn := func(ctx context.Context, conversation string) (string, error) {
		summarizeCalls.Add(1)
		return "summary of: " + conversation[:min(20, len(conversation))], nil
	}

	store, _ := NewStore(filepath.Join(t.TempDir(), "recall.db"))
	t.Cleanup(func() { store.Close() })
	m := New(store, WithQueue(queue), WithSummarizer(NewSummarizer(store, fn, 50)), WithSummarizeEvery(2))
	bus := ipc.NewBus()
	t.Cleanup(bus.Close)
	m.Init(context.Background(), &kernel.Services{Bus: bus})
	m.Start(context.Background()) // registers the summarize job handler on the queue

	// One turn indexes user+assistant = 2 messages, hitting the every=2 threshold.
	m.handleTurnCompleted(turn("acme", "jane", "s1", "let's talk about the budget", "ok, the budget is set"))

	if pending, _ := queue.CountByState("acme", jobs.StatePending); pending != 1 {
		t.Fatalf("expected 1 pending summarize job, got %d", pending)
	}

	queue.RunOnce(context.Background())

	if summarizeCalls.Load() != 1 {
		t.Fatalf("summarizer called %d times, want 1", summarizeCalls.Load())
	}
	summary, count, _ := store.Summary("acme", "jane", "s1")
	if summary == "" {
		t.Fatal("expected a stored summary after the job ran")
	}
	if count != 2 {
		t.Fatalf("summary covered %d messages, want 2", count)
	}

	// Below threshold again right after summarizing — no new job.
	m.handleTurnCompleted(turn("acme", "jane", "s1", "small follow up", "noted"))
	// total=4, summarized=2, delta=2 >= 2 -> a new job IS expected here.
	if pending, _ := queue.CountByState("acme", jobs.StatePending); pending != 1 {
		t.Fatalf("expected exactly 1 new pending job after crossing threshold again, got %d", pending)
	}
}

func TestModuleIgnoresMalformedAndScopelessEvents(t *testing.T) {
	m, _ := startModule(t)
	// Wrong payload type — must not panic or error.
	if _, err := m.handleTurnCompleted(ipc.Message{Topic: agent.TopicTurnCompleted, Payload: "nope"}); err != nil {
		t.Fatalf("malformed payload should be ignored, got %v", err)
	}
	// Missing scope — nothing indexed.
	m.handleTurnCompleted(turn("acme", "", "s1", "hi", "hello"))
	if n, _ := m.store.MessageCount("acme", "", "s1"); n != 0 {
		t.Fatalf("scopeless event was indexed (%d rows)", n)
	}
}
