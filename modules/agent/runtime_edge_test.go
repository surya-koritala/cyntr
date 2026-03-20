package agent_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/agent/providers"
)

// alwaysToolCallProvider is a model provider that always returns a tool call,
// never a plain-text response — used to drive MaxTurns exhaustion.
type alwaysToolCallProvider struct{}

func (a *alwaysToolCallProvider) Name() string { return "always_tool" }

func (a *alwaysToolCallProvider) Chat(
	ctx context.Context,
	messages []agent.Message,
	tools []agent.ToolDef,
) (agent.Message, error) {
	return agent.Message{
		Role: agent.RoleAssistant,
		ToolCalls: []agent.ToolCall{
			{ID: "call_loop_001", Name: "echo", Input: map[string]string{"text": "loop"}},
		},
	}, nil
}

// newTestRuntime is a helper that wires up a Runtime with a bus and returns
// the bus plus a cancel func for the runtime lifecycle.
func newTestRuntime(t *testing.T) (*agent.Runtime, *ipc.Bus) {
	t.Helper()
	bus := ipc.NewBus()
	rt := agent.NewRuntime()
	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	t.Cleanup(func() {
		rt.Stop(ctx)
		bus.Close()
	})
	return rt, bus
}

// TestRuntimeChatNoProviderRegistered verifies that chatting with an agent whose
// model has no registered provider returns an error.
func TestRuntimeChatNoProviderRegistered(t *testing.T) {
	_, bus := newTestRuntime(t)
	// No provider registered — model "nonexistent" is unknown.

	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Create the agent first (create succeeds regardless of provider).
	_, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: "ghost-agent", Tenant: "finance", Model: "nonexistent", MaxTurns: 5,
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Chat should fail because the provider is not registered.
	_, err = bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{Agent: "ghost-agent", Tenant: "finance", Message: "Hi"},
	})
	if err == nil {
		t.Fatal("expected error for missing provider, got nil")
	}
}

// TestRuntimeChatMaxTurnsExceeded verifies that an agentic loop that never
// settles (always returning tool calls) hits the MaxTurns limit and errors.
func TestRuntimeChatMaxTurnsExceeded(t *testing.T) {
	rt, bus := newTestRuntime(t)
	rt.RegisterProvider(&alwaysToolCallProvider{})

	toolReg := agent.NewToolRegistry()
	toolReg.Register(&echoToolImpl{}) // same echo tool used in runtime_test.go
	rt.SetToolRegistry(toolReg)

	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: "loop-agent", Tenant: "ops", Model: "always_tool",
			Tools: []string{"echo"}, MaxTurns: 1,
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{Agent: "loop-agent", Tenant: "ops", Message: "Go!"},
	})
	if err == nil {
		t.Fatal("expected max-turns error, got nil")
	}
}

// TestRuntimeChatEmptyMessage verifies that an empty user message is accepted
// and produces a response (the model handles the empty content).
func TestRuntimeChatEmptyMessage(t *testing.T) {
	rt, bus := newTestRuntime(t)
	rt.RegisterProvider(providers.NewMock("I heard nothing, but I am here."))

	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: "quiet-agent", Tenant: "ops", Model: "mock", MaxTurns: 5,
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{Agent: "quiet-agent", Tenant: "ops", Message: ""},
	})
	if err != nil {
		t.Fatalf("chat with empty message: %v", err)
	}

	chatResp, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		t.Fatalf("expected ChatResponse, got %T", resp.Payload)
	}
	if chatResp.Content == "" {
		t.Fatal("expected non-empty content from mock provider")
	}
}

// TestRuntimeCreateDuplicateAgent verifies that creating the same agent twice
// overwrites the previous instance (current documented behavior).
func TestRuntimeCreateDuplicateAgent(t *testing.T) {
	rt, bus := newTestRuntime(t)
	rt.RegisterProvider(providers.NewMock("first"))

	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cfg := agent.AgentConfig{
		Name: "dup-agent", Tenant: "finance", Model: "mock", MaxTurns: 5,
		SystemPrompt: "First version.",
	}

	// First create
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: cfg,
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("first create: expected ok, got %v", resp.Payload)
	}

	// Second create — same tenant/name, different system prompt
	cfg.SystemPrompt = "Second version."
	resp, err = bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: cfg,
	})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("second create: expected ok, got %v", resp.Payload)
	}

	// List — should still be just one agent with that name
	listResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.list",
		Payload: "finance",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	names := listResp.Payload.([]string)
	count := 0
	for _, n := range names {
		if n == "dup-agent" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 dup-agent in list, got %d (names: %v)", count, names)
	}
}

// TestRuntimeChatConcurrent fires 10 goroutines chatting with the same agent
// simultaneously and asserts there are no panics or data races.
func TestRuntimeChatConcurrent(t *testing.T) {
	rt, bus := newTestRuntime(t)
	rt.RegisterProvider(providers.NewMock("concurrent reply"))

	reqCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: "conc-agent", Tenant: "finance", Model: "mock", MaxTurns: 10,
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	const workers = 10
	var wg sync.WaitGroup
	errs := make([]error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, chatErr := bus.Request(reqCtx, ipc.Message{
				Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
				Payload: agent.ChatRequest{
					Agent:   "conc-agent",
					Tenant:  "finance",
					User:    fmt.Sprintf("user%d@corp.com", idx),
					Message: fmt.Sprintf("message %d", idx),
				},
			})
			errs[idx] = chatErr
		}(i)
	}

	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, e)
		}
	}
}

// TestRuntimeListEmptyTenant verifies that listing agents for a tenant that has
// no registered agents returns an empty (non-nil) slice.
func TestRuntimeListEmptyTenant(t *testing.T) {
	_, bus := newTestRuntime(t)

	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.list",
		Payload: "totally-unknown-tenant",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	names, ok := resp.Payload.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", resp.Payload)
	}
	if len(names) != 0 {
		t.Fatalf("expected 0 agents, got %d: %v", len(names), names)
	}
}

// TestRuntimeSessionPersistence verifies that when a SessionStore is attached
// to the runtime, messages are written to the store during a chat interaction.
func TestRuntimeSessionPersistence(t *testing.T) {
	rt, bus := newTestRuntime(t)
	rt.RegisterProvider(providers.NewMock("Persisted response"))

	// Use an in-memory SQLite database (":memory:" is per-connection, use a
	// temp file so the store and any later load see the same DB).
	tmpDB := t.TempDir() + "/session_persist_test.db"
	store, err := agent.NewSessionStore(tmpDB)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	rt.SetSessionStore(store)

	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: "persist-agent", Tenant: "finance", Model: "mock", MaxTurns: 5,
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent: "persist-agent", Tenant: "finance", User: "alice@corp.com",
			Message: "Hello, persist me!",
		},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	// The store should now have at least one session with at least two messages
	// (user message + assistant response).
	ids, err := store.ListSessions()
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(ids) == 0 {
		t.Fatal("expected at least one session in the store after chat")
	}

	_, messages, err := store.LoadSession(ids[0])
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if len(messages) < 2 {
		t.Fatalf("expected at least 2 messages (user + assistant), got %d", len(messages))
	}
}
