package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/agent/providers"
)

// startRuntimeWithAgent wires a Runtime onto a fresh bus, registers the given
// provider, and creates one agent. It returns the bus so the caller can
// subscribe to events and drive chats.
func startRuntimeWithAgent(t *testing.T, provider agent.ModelProvider, cfg agent.AgentConfig, toolReg *agent.ToolRegistry) *ipc.Bus {
	t.Helper()
	bus := ipc.NewBus()
	t.Cleanup(bus.Close)

	rt := agent.NewRuntime()
	rt.RegisterProvider(provider)
	if toolReg != nil {
		rt.SetToolRegistry(toolReg)
	}

	ctx := context.Background()
	if err := rt.Init(ctx, &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := rt.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { rt.Stop(ctx) })

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if _, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create", Payload: cfg,
	}); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	return bus
}

// subscribeTurns subscribes to TopicTurnCompleted and returns a channel of
// the decoded records.
func subscribeTurns(bus *ipc.Bus) chan agent.TurnRecord {
	ch := make(chan agent.TurnRecord, 8)
	bus.Subscribe("test-observer", agent.TopicTurnCompleted, func(msg ipc.Message) (ipc.Message, error) {
		if rec, ok := msg.Payload.(agent.TurnRecord); ok {
			ch <- rec
		}
		return ipc.Message{}, nil
	})
	return ch
}

func chat(t *testing.T, bus *ipc.Bus, req agent.ChatRequest) agent.ChatResponse {
	t.Helper()
	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat", Payload: req,
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	cr, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		t.Fatalf("expected ChatResponse, got %T", resp.Payload)
	}
	return cr
}

func waitForTurn(t *testing.T, ch chan agent.TurnRecord) agent.TurnRecord {
	t.Helper()
	select {
	case rec := <-ch:
		return rec
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for agent.turn_completed event")
		return agent.TurnRecord{}
	}
}

// A plain chat (no tool calls) emits exactly one turn_completed event with a
// well-formed record.
func TestTurnCompletedEventEmitted(t *testing.T) {
	bus := startRuntimeWithAgent(t, providers.NewMock("Hello from the agent!"),
		agent.AgentConfig{Name: "obs-agent", Tenant: "finance", Model: "mock", MaxTurns: 10}, nil)
	ch := subscribeTurns(bus)

	cr := chat(t, bus, agent.ChatRequest{Agent: "obs-agent", Tenant: "finance", User: "jane@corp.com", Message: "Hi"})
	if cr.Content != "Hello from the agent!" {
		t.Fatalf("unexpected response: %q", cr.Content)
	}

	rec := waitForTurn(t, ch)
	if rec.Tenant != "finance" || rec.User != "jane@corp.com" || rec.Agent != "obs-agent" {
		t.Fatalf("identity fields wrong: %+v", rec)
	}
	if rec.Model != "mock" {
		t.Fatalf("model = %q, want mock", rec.Model)
	}
	if rec.Outcome != "ok" {
		t.Fatalf("outcome = %q, want ok", rec.Outcome)
	}
	if rec.Turns != 1 {
		t.Fatalf("turns = %d, want 1", rec.Turns)
	}
	if rec.ToolCalls != 0 || len(rec.ToolsUsed) != 0 {
		t.Fatalf("expected no tool calls, got calls=%d used=%v", rec.ToolCalls, rec.ToolsUsed)
	}
	if rec.UserMessage != "Hi" {
		t.Fatalf("user message = %q", rec.UserMessage)
	}
	if rec.Response != "Hello from the agent!" {
		t.Fatalf("response = %q (should be raw, pre-sanitization)", rec.Response)
	}
	if rec.Session == "" {
		t.Fatal("session id should be populated")
	}
	if rec.StartedAt.IsZero() {
		t.Fatal("StartedAt should be set")
	}
	if rec.DurationMS < 0 {
		t.Fatalf("duration = %d", rec.DurationMS)
	}

	// Exactly one event — no duplicate on the same turn.
	select {
	case extra := <-ch:
		t.Fatalf("expected one event, got a second: %+v", extra)
	case <-time.After(150 * time.Millisecond):
	}
}

// A turn that invokes a tool reports the tool usage and the extra model turn.
func TestTurnCompletedEventWithToolUse(t *testing.T) {
	toolReg := agent.NewToolRegistry()
	toolReg.Register(&echoToolImpl{})
	bus := startRuntimeWithAgent(t, providers.NewMockWithToolCall("echo", map[string]string{"text": "test"}),
		agent.AgentConfig{Name: "tool-agent", Tenant: "finance", Model: "mock", Tools: []string{"echo"}, MaxTurns: 10}, toolReg)
	ch := subscribeTurns(bus)

	chat(t, bus, agent.ChatRequest{Agent: "tool-agent", Tenant: "finance", User: "jane@corp.com", Message: "Echo something"})

	rec := waitForTurn(t, ch)
	if rec.ToolCalls != 1 {
		t.Fatalf("tool calls = %d, want 1", rec.ToolCalls)
	}
	if len(rec.ToolsUsed) != 1 || rec.ToolsUsed[0] != "echo" {
		t.Fatalf("tools used = %v, want [echo]", rec.ToolsUsed)
	}
	if rec.Turns != 2 {
		t.Fatalf("turns = %d, want 2 (tool turn + final)", rec.Turns)
	}
}

// A panicking subscriber must not break the user's response or stop other
// subscribers from receiving the event (bus delivers each in a recovered
// goroutine).
func TestTurnCompletedSurvivesPanickingSubscriber(t *testing.T) {
	bus := startRuntimeWithAgent(t, providers.NewMock("ok response"),
		agent.AgentConfig{Name: "obs-agent", Tenant: "acme", Model: "mock", MaxTurns: 10}, nil)

	bus.Subscribe("bad-observer", agent.TopicTurnCompleted, func(msg ipc.Message) (ipc.Message, error) {
		panic("boom")
	})
	good := subscribeTurns(bus)

	cr := chat(t, bus, agent.ChatRequest{Agent: "obs-agent", Tenant: "acme", User: "u@corp.com", Message: "Hi"})
	if cr.Content != "ok response" {
		t.Fatalf("user response should be unaffected by a panicking subscriber, got %q", cr.Content)
	}

	rec := waitForTurn(t, good)
	if rec.Agent != "obs-agent" {
		t.Fatalf("good subscriber should still receive the event, got %+v", rec)
	}
}
