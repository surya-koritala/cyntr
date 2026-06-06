package tools

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func TestContextReadToolMeta(t *testing.T) {
	tool := NewContextReadTool(nil)
	if tool.Name() != "context_read" {
		t.Fatalf("name = %q", tool.Name())
	}
	if len(tool.Parameters()) != 0 {
		t.Fatal("context_read takes no params (channel comes from context)")
	}
}

// runtimeWithContext spins up a real agent.Runtime with a shared-context store
// so the actual TopicContextWrite/Read handlers run.
func runtimeWithContext(t *testing.T) (*agent.Runtime, *ipc.Bus) {
	t.Helper()
	bus := ipc.NewBus()
	rt := agent.NewRuntime()
	cs, err := agent.NewContextStore(t.TempDir() + "/sc.db")
	if err != nil {
		t.Fatalf("NewContextStore: %v", err)
	}
	rt.SetContextStore(cs)
	ctx := context.Background()
	if err := rt.Init(ctx, &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { rt.Stop(ctx); cs.Close(); bus.Close() })
	return rt, bus
}

func writeNote(t *testing.T, bus *ipc.Bus, tenant, channel, key, content, author string) {
	t.Helper()
	_, err := bus.Request(context.Background(), ipc.Message{
		Source: "orchestrate", Target: "agent_runtime", Topic: agent.TopicContextWrite,
		Payload: agent.SharedContextEntry{Tenant: tenant, Channel: channel, Key: key, Content: content, Author: author},
	})
	if err != nil {
		t.Fatalf("write note: %v", err)
	}
}

// workerCtx mimics what the runtime hands a worker subagent's tools: the
// caller's tenant plus the orchestration channel id.
func workerCtx(tenant, channel string) context.Context {
	return agent.WithChannel(agent.WithToolCaller(context.Background(), tenant, "worker", "u"), channel)
}

func TestContextReadWorkerSeesCoordinatorNote(t *testing.T) {
	_, bus := runtimeWithContext(t)
	writeNote(t, bus, "acme", "batch1", "plan", "1. design 2. build", "architect")

	tool := NewContextReadTool(bus)
	out, err := tool.Execute(workerCtx("acme", "batch1"), nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "1. design 2. build") || !strings.Contains(out, "architect") {
		t.Fatalf("worker did not see the coordinator's note: %q", out)
	}
}

func TestContextReadIsTenantAndChannelScoped(t *testing.T) {
	_, bus := runtimeWithContext(t)
	writeNote(t, bus, "acme", "batch1", "plan", "secret plan", "architect")
	tool := NewContextReadTool(bus)

	// Wrong tenant — must not see acme's note.
	if out, _ := tool.Execute(workerCtx("globex", "batch1"), nil); strings.Contains(out, "secret plan") {
		t.Fatalf("cross-tenant leak: %q", out)
	}
	// Wrong channel — same tenant, different batch.
	if out, _ := tool.Execute(workerCtx("acme", "other-batch"), nil); strings.Contains(out, "secret plan") {
		t.Fatalf("cross-channel leak: %q", out)
	}
}

func TestContextReadNoChannelReturnsGuidance(t *testing.T) {
	_, bus := runtimeWithContext(t)
	tool := NewContextReadTool(bus)
	// A top-level (non-orchestrated) turn has no channel bound.
	ctx := agent.WithToolCaller(context.Background(), "acme", "solo", "u")
	out, err := tool.Execute(ctx, nil)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if strings.Contains(out, "secret") || out == "" {
		t.Fatalf("no-channel read should return empty guidance, got %q", out)
	}
}

// captureContextWrites is a fake agent_runtime that records shared-context
// writes and echoes chat, so we can assert the coordinator-write half of
// orchestrate without a full runtime.
type captureContextWrites struct {
	mu     sync.Mutex
	writes []agent.SharedContextEntry
	traces []string
}

func contextWriteBus(t *testing.T, c *captureContextWrites) *ipc.Bus {
	t.Helper()
	bus := ipc.NewBus()
	t.Cleanup(bus.Close)
	bus.Handle("agent_runtime", agent.TopicContextWrite, func(msg ipc.Message) (ipc.Message, error) {
		e := msg.Payload.(agent.SharedContextEntry)
		c.mu.Lock()
		c.writes = append(c.writes, e)
		c.mu.Unlock()
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
	})
	bus.Handle("agent_runtime", "agent.chat", func(msg ipc.Message) (ipc.Message, error) {
		req := msg.Payload.(agent.ChatRequest)
		c.mu.Lock()
		c.traces = append(c.traces, msg.TraceID)
		c.mu.Unlock()
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: agent.ChatResponse{Agent: req.Agent, Content: "ok"}}, nil
	})
	return bus
}

func TestOrchestrateWritesSharedContextThenFansOut(t *testing.T) {
	c := &captureContextWrites{}
	tool := NewOrchestrateTool(contextWriteBus(t, c))
	_, err := tool.Execute(callerCtx("acme", "jane"), map[string]string{
		"tasks":          `[{"agent":"impl","message":"build it"}]`,
		"shared_context": `{"plan":"do X then Y"}`,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(c.writes) != 1 {
		t.Fatalf("expected 1 shared-context write, got %d", len(c.writes))
	}
	w := c.writes[0]
	if w.Tenant != "acme" || w.Key != "plan" || w.Content != "do X then Y" || w.Author != "parent" {
		t.Fatalf("write wrong: %+v", w)
	}
	// The child's chat must carry the SAME channel id the note was written under.
	if len(c.traces) != 1 || c.traces[0] != w.Channel || w.Channel == "" {
		t.Fatalf("child trace %v must equal note channel %q", c.traces, w.Channel)
	}
}

func TestOrchestrateRejectsMalformedSharedContext(t *testing.T) {
	c := &captureContextWrites{}
	tool := NewOrchestrateTool(contextWriteBus(t, c))
	_, err := tool.Execute(callerCtx("acme", "jane"), map[string]string{
		"tasks":          `[{"agent":"impl","message":"x"}]`,
		"shared_context": `not json`,
	})
	if err == nil {
		t.Fatal("malformed shared_context should be a hard error")
	}
	if len(c.writes) != 0 {
		t.Fatalf("nothing should be written on parse failure, got %d", len(c.writes))
	}
}
