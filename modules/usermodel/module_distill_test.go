package usermodel

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func newWiredModule(t *testing.T, provider LLMProvider) (*Module, *Store, *ipc.Bus, *Distiller) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "usermodel.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	d, err := NewDistiller(DistillerOptions{Store: s, Provider: provider, Model: "fake"})
	if err != nil {
		t.Fatalf("new distiller: %v", err)
	}

	bus := ipc.NewBus()
	t.Cleanup(func() { bus.Close() })

	m := New(s)
	m.SetDistiller(d)
	if err := m.Init(context.Background(), &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { m.Stop(context.Background()) })
	return m, s, bus, d
}

func TestIPCDistillHandlerReturnsResult(t *testing.T) {
	fp := &fakeProvider{response: "## P\nfresh content"}
	_, s, bus, _ := newWiredModule(t, fp)
	if err := s.SetTenantDistillEnabled("acme", true); err != nil {
		t.Fatal(err)
	}
	seedActivity(t, s, "acme", "alice", 5)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "usermodel", Topic: TopicDistill,
		Payload: map[string]string{"tenant": "acme", "user": "alice"},
	})
	if err != nil {
		t.Fatalf("ipc request: %v", err)
	}
	res, ok := resp.Payload.(DistillResult)
	if !ok {
		t.Fatalf("expected DistillResult, got %T", resp.Payload)
	}
	if res.Tenant != "acme" || res.User != "alice" {
		t.Errorf("unexpected ids: %+v", res)
	}
	if res.NewSize == 0 {
		t.Errorf("expected new_size > 0, got %+v", res)
	}
}

func TestIPCRecordActivityPersists(t *testing.T) {
	fp := &fakeProvider{response: "## P\nx"}
	_, s, bus, _ := newWiredModule(t, fp)

	if err := bus.Publish(ipc.Message{
		Source: "test", Target: "usermodel", Topic: TopicRecordActivity,
		Type:    ipc.MessageTypeEvent,
		Payload: map[string]string{"tenant": "acme", "user": "alice", "summary": "user asked about Go"},
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Subscribe handlers run async — poll briefly.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		acts, _ := s.RecentActivity("acme", "alice", 5)
		if len(acts) > 0 {
			if acts[0].Summary != "user asked about Go" {
				t.Errorf("unexpected summary: %q", acts[0].Summary)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("activity row never appeared")
}

// scheduledProvider increments a counter on each invocation so the
// scheduler-integration test can assert the tick actually called the LLM
// for every eligible user.
type scheduledProvider struct {
	mu       sync.Mutex
	calls    map[string]int
	response string
}

func (sp *scheduledProvider) Name() string { return "scheduled" }

func (sp *scheduledProvider) DistillChat(ctx context.Context, msgs []DistillMessage) (string, int, int, error) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	// Tag the call by content fingerprint — the user identity ends up in
	// the activity summaries we prepended into the prompt.
	body := ""
	if len(msgs) > 0 {
		body = msgs[0].Content
	}
	sp.calls[body[:min(60, len(body))]]++
	return sp.response, 1, 1, nil
}

func (sp *scheduledProvider) totalCalls() int {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	n := 0
	for _, v := range sp.calls {
		n += v
	}
	return n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestSchedulerTickerFiresDistiller(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "usermodel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetTenantDistillEnabled("acme", true)
	seedActivity(t, s, "acme", "alice", 5)
	seedActivity(t, s, "acme", "bob", 4)

	sp := &scheduledProvider{calls: map[string]int{}, response: "## P\nresult"}
	d, err := NewDistiller(DistillerOptions{Store: s, Provider: sp, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}

	// Directly call Tick (skip the cron-matcher) to verify the orchestration
	// — the cron parser is covered by its own unit test.
	results := d.Tick(context.Background())
	if len(results) < 2 {
		t.Fatalf("expected >=2 results from tick, got %d", len(results))
	}
	if sp.totalCalls() < 2 {
		t.Errorf("expected >=2 LLM calls, got %d", sp.totalCalls())
	}

	// Both users should now have a stamped last_distilled_at.
	for _, user := range []string{"alice", "bob"} {
		ts, err := s.LastDistilledAt("acme", user)
		if err != nil {
			t.Fatalf("last_distilled_at %s: %v", user, err)
		}
		if ts == 0 {
			t.Errorf("expected last_distilled_at > 0 for %s", user)
		}
	}
}
