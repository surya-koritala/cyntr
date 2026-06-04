package learn

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/jobs"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/skill"
)

func complexTurn() agent.TurnRecord {
	return agent.TurnRecord{
		Tenant: "acme", User: "jane", Agent: "assistant", Session: "s1",
		UserMessage: "diagnose the cost spike", Response: "found it: a runaway job",
		ToolsUsed: []string{"aws_cost", "kubectl"}, ToolCalls: 3, Turns: 4, Outcome: "ok",
	}
}

// captureBus registers fake handlers for the downstream topics and returns
// channels that receive whatever the learn module emits.
func captureBus(t *testing.T) (*ipc.Bus, chan agent.Memory, chan skill.ProposeRequest) {
	t.Helper()
	bus := ipc.NewBus()
	t.Cleanup(bus.Close)
	mem := make(chan agent.Memory, 4)
	prop := make(chan skill.ProposeRequest, 4)
	bus.Handle("agent_runtime", "agent.memory.save", func(msg ipc.Message) (ipc.Message, error) {
		mem <- msg.Payload.(agent.Memory)
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
	})
	bus.Handle("skill_runtime", skill.TopicPropose, func(msg ipc.Message) (ipc.Message, error) {
		prop <- msg.Payload.(skill.ProposeRequest)
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: skill.ProposeResult{ID: 1, Status: skill.CandidatePending}}, nil
	})
	return bus, mem, prop
}

func newModule(t *testing.T, bus *ipc.Bus, fn ReflectFunc, opts ...Option) *Module {
	t.Helper()
	m := New(true, append([]Option{WithReflectFunc(fn)}, opts...)...)
	if err := m.Init(context.Background(), &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return m
}

func TestShouldReflect(t *testing.T) {
	if !shouldReflect(complexTurn(), 3) {
		t.Fatal("3 tool calls should qualify")
	}
	trivial := complexTurn()
	trivial.ToolCalls = 1
	if shouldReflect(trivial, 3) {
		t.Fatal("1 tool call should not qualify")
	}
	noScope := complexTurn()
	noScope.Tenant = ""
	if shouldReflect(noScope, 3) {
		t.Fatal("missing tenant should not qualify")
	}
}

func TestReflectJobEmitsMemoryAndSkill(t *testing.T) {
	bus, mem, prop := captureBus(t)
	fn := func(ctx context.Context, prompt string) (string, error) {
		return `{"memory":"cost spikes often come from runaway jobs","skill":{"name":"diagnose-cost-spike","description":"find a cost spike","instructions":"# Steps\n1. check cost explorer"}}`, nil
	}
	m := newModule(t, bus, fn)

	payload, _ := json.Marshal(complexTurn())
	if err := m.handleReflectJob(context.Background(), jobs.Job{Payload: payload}); err != nil {
		t.Fatalf("handleReflectJob: %v", err)
	}

	select {
	case got := <-mem:
		if got.Tenant != "acme" || got.Agent != "assistant" || got.Content == "" {
			t.Fatalf("memory wrong: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no memory saved")
	}
	select {
	case got := <-prop:
		if got.Name != "diagnose-cost-spike" || got.Tenant != "acme" {
			t.Fatalf("skill proposal wrong: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no skill proposed")
	}
}

func TestReflectJobMemoryOnly(t *testing.T) {
	bus, mem, prop := captureBus(t)
	fn := func(ctx context.Context, prompt string) (string, error) {
		return "```json\n{\"memory\":\"just a fact\"}\n```", nil
	}
	m := newModule(t, bus, fn)
	payload, _ := json.Marshal(complexTurn())
	m.handleReflectJob(context.Background(), jobs.Job{Payload: payload})

	if got := <-mem; got.Content != "just a fact" {
		t.Fatalf("memory = %q", got.Content)
	}
	select {
	case p := <-prop:
		t.Fatalf("did not expect a skill proposal, got %+v", p)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestReflectJobEmptyMemoryNoWrites(t *testing.T) {
	bus, mem, _ := captureBus(t)
	fn := func(ctx context.Context, prompt string) (string, error) { return `{"memory":""}`, nil }
	m := newModule(t, bus, fn)
	payload, _ := json.Marshal(complexTurn())
	m.handleReflectJob(context.Background(), jobs.Job{Payload: payload})

	select {
	case got := <-mem:
		t.Fatalf("empty reflection should write nothing, got %+v", got)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestComplexTurnEnqueuesAndRuns(t *testing.T) {
	bus, mem, _ := captureBus(t)
	q, err := jobs.NewQueue(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	t.Cleanup(func() { q.Close() })
	fn := func(ctx context.Context, prompt string) (string, error) {
		return `{"memory":"learned something"}`, nil
	}
	m := newModule(t, bus, fn, WithQueue(q))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// A complex turn enqueues a reflection.
	m.handleTurnCompleted(ipc.Message{Topic: agent.TopicTurnCompleted, Payload: complexTurn()})
	if n, _ := q.CountByState("acme", jobs.StatePending); n != 1 {
		t.Fatalf("expected 1 pending reflection, got %d", n)
	}
	// A trivial turn does not.
	trivial := complexTurn()
	trivial.ToolCalls = 1
	m.handleTurnCompleted(ipc.Message{Topic: agent.TopicTurnCompleted, Payload: trivial})
	if n, _ := q.CountByState("acme", jobs.StatePending); n != 1 {
		t.Fatalf("trivial turn should not enqueue; pending = %d", n)
	}

	q.RunOnce(context.Background())
	if got := <-mem; got.Content == "" {
		t.Fatal("reflection job did not save a memory")
	}
}

func TestDisabledRegistersNoHandler(t *testing.T) {
	bus := ipc.NewBus()
	t.Cleanup(bus.Close)
	q, _ := jobs.NewQueue(filepath.Join(t.TempDir(), "jobs.db"))
	t.Cleanup(func() { q.Close() })
	fn := func(ctx context.Context, prompt string) (string, error) { return `{"memory":"x"}`, nil }

	m := New(false, WithQueue(q), WithReflectFunc(fn)) // disabled
	m.Init(context.Background(), &kernel.Services{Bus: bus})
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if m.active() {
		t.Fatal("module should be inactive when disabled")
	}
	// No reflect handler registered -> a manually enqueued job is never claimed.
	q.Enqueue("acme", JobKindReflect, []byte("{}"), time.Time{})
	q.RunOnce(context.Background())
	if n, _ := q.CountByState("acme", jobs.StatePending); n != 1 {
		t.Fatalf("disabled loop should not process reflect jobs; pending = %d", n)
	}
}
