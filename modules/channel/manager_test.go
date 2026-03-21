package channel

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

// mockAdapter for testing the manager
type mockAdapter struct {
	name    string
	handler InboundHandler
	sent    []OutboundMessage
	mu      sync.Mutex
}

func (m *mockAdapter) Name() string { return m.name }
func (m *mockAdapter) Start(ctx context.Context, handler InboundHandler) error {
	m.handler = handler
	return nil
}
func (m *mockAdapter) Stop(ctx context.Context) error { return nil }
func (m *mockAdapter) Send(ctx context.Context, msg OutboundMessage) error {
	m.mu.Lock()
	m.sent = append(m.sent, msg)
	m.mu.Unlock()
	return nil
}
func (m *mockAdapter) Sent() []OutboundMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sent
}

func TestManagerImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Manager)(nil)
}

func TestManagerRoutesInboundToAgent(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	// Register a mock agent handler
	bus.Handle("agent_runtime", "agent.chat", func(msg ipc.Message) (ipc.Message, error) {
		req := msg.Payload.(agent.ChatRequest)
		return ipc.Message{
			Type: ipc.MessageTypeResponse,
			Payload: agent.ChatResponse{
				Agent:   req.Agent,
				Content: "Agent says hello!",
			},
		}, nil
	})

	adapter := &mockAdapter{name: "test-channel"}
	mgr := NewManager()
	mgr.AddAdapter(adapter)

	ctx := context.Background()
	mgr.Init(ctx, &kernel.Services{Bus: bus})
	mgr.Start(ctx)
	defer mgr.Stop(ctx)

	// Simulate inbound message through the adapter
	response, err := adapter.handler(InboundMessage{
		Channel: "test-channel", ChannelID: "C123", UserID: "U456",
		Text: "Hello", Tenant: "marketing", Agent: "assistant",
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if response != "Agent says hello!" {
		t.Fatalf("expected agent response, got %q", response)
	}
}

func TestManagerSendViaIPC(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	adapter := &mockAdapter{name: "test-channel"}
	mgr := NewManager()
	mgr.AddAdapter(adapter)

	ctx := context.Background()
	mgr.Init(ctx, &kernel.Services{Bus: bus})
	mgr.Start(ctx)
	defer mgr.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "agent_runtime", Target: "channel", Topic: "channel.send",
		Payload: OutboundMessage{Channel: "test-channel", ChannelID: "C123", Text: "Reply from agent"},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("expected ok, got %v", resp.Payload)
	}

	time.Sleep(100 * time.Millisecond)

	sent := adapter.Sent()
	if len(sent) != 1 {
		t.Fatalf("expected 1 sent, got %d", len(sent))
	}
	if sent[0].Text != "Reply from agent" {
		t.Fatalf("expected reply, got %q", sent[0].Text)
	}
}

func TestManagerListChannels(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	mgr := NewManager()
	mgr.AddAdapter(&mockAdapter{name: "slack"})
	mgr.AddAdapter(&mockAdapter{name: "teams"})

	ctx := context.Background()
	mgr.Init(ctx, &kernel.Services{Bus: bus})
	mgr.Start(ctx)
	defer mgr.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "channel", Topic: "channel.list",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	names, ok := resp.Payload.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", resp.Payload)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2, got %d", len(names))
	}
}

func TestManagerHealthy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	mgr := NewManager()
	ctx := context.Background()
	mgr.Init(ctx, &kernel.Services{Bus: bus})
	mgr.Start(ctx)
	defer mgr.Stop(ctx)

	h := mgr.Health(ctx)
	if !h.Healthy {
		t.Fatalf("expected healthy: %s", h.Message)
	}
}

// setupManagerWithMocks creates a Manager with a mock agent handler and a test adapter.
func setupManagerWithMocks(t *testing.T) (*Manager, *mockAdapter, *ipc.Bus) {
	t.Helper()
	bus := ipc.NewBus()

	bus.Handle("agent_runtime", "agent.chat", func(msg ipc.Message) (ipc.Message, error) {
		req := msg.Payload.(agent.ChatRequest)
		return ipc.Message{
			Type: ipc.MessageTypeResponse,
			Payload: agent.ChatResponse{
				Agent:   req.Agent,
				Content: "reply to: " + req.Message,
			},
		}, nil
	})

	bus.Handle("agent_runtime", "agent.session.clear", func(msg ipc.Message) (ipc.Message, error) {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "cleared"}, nil
	})

	adapter := &mockAdapter{name: "test-channel"}
	mgr := NewManager()
	mgr.AddAdapter(adapter)

	ctx := context.Background()
	mgr.Init(ctx, &kernel.Services{Bus: bus})
	mgr.Start(ctx)

	t.Cleanup(func() { mgr.Stop(ctx); bus.Close() })
	return mgr, adapter, bus
}

func TestRouteInboundClearCommand(t *testing.T) {
	_, adapter, _ := setupManagerWithMocks(t)

	clearCommands := []string{"clear", "reset", "/clear", "/reset", "new conversation"}
	for _, cmd := range clearCommands {
		response, err := adapter.handler(InboundMessage{
			Text: cmd, Tenant: "t", Agent: "a",
		})
		if err != nil {
			t.Fatalf("cmd %q: %v", cmd, err)
		}
		if response != "Session cleared. Starting fresh conversation." {
			t.Fatalf("cmd %q: expected clear response, got %q", cmd, response)
		}
	}
}

func TestRouteInboundClearCommandCaseInsensitive(t *testing.T) {
	_, adapter, _ := setupManagerWithMocks(t)

	// Test that case does not matter (the code lowercases before comparing)
	for _, cmd := range []string{"CLEAR", "Reset", "  clear  ", "  /CLEAR  "} {
		response, err := adapter.handler(InboundMessage{
			Text: cmd, Tenant: "t", Agent: "a",
		})
		if err != nil {
			t.Fatalf("cmd %q: %v", cmd, err)
		}
		if response != "Session cleared. Starting fresh conversation." {
			t.Fatalf("cmd %q: expected clear response, got %q", cmd, response)
		}
	}
}

func TestRouteInboundNormalMessage(t *testing.T) {
	_, adapter, _ := setupManagerWithMocks(t)

	response, err := adapter.handler(InboundMessage{
		Text: "hello", Tenant: "t", Agent: "a",
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	// Mock agent responds with "reply to: hello"
	if response == "" {
		t.Fatal("expected non-empty response")
	}
	if response == "Session cleared. Starting fresh conversation." {
		t.Fatal("normal message should not trigger clear")
	}
}

func TestRouteInboundNonClearCommandsNotIntercepted(t *testing.T) {
	_, adapter, _ := setupManagerWithMocks(t)

	// These should NOT trigger clear
	nonClearMessages := []string{
		"please clear the table",
		"reset my password",
		"can you clear this up",
		"clearing things out",
		"/help",
		"hello",
	}
	for _, msg := range nonClearMessages {
		response, err := adapter.handler(InboundMessage{
			Text: msg, Tenant: "t", Agent: "a",
		})
		if err != nil {
			t.Fatalf("msg %q: %v", msg, err)
		}
		if response == "Session cleared. Starting fresh conversation." {
			t.Fatalf("msg %q: should not trigger clear", msg)
		}
	}
}

func TestRouteInboundClearWithSessionClearError(t *testing.T) {
	bus := ipc.NewBus()

	// Register a session clear handler that fails
	bus.Handle("agent_runtime", "agent.session.clear", func(msg ipc.Message) (ipc.Message, error) {
		return ipc.Message{}, fmt.Errorf("session store unavailable")
	})

	adapter := &mockAdapter{name: "test-channel"}
	mgr := NewManager()
	mgr.AddAdapter(adapter)

	ctx := context.Background()
	mgr.Init(ctx, &kernel.Services{Bus: bus})
	mgr.Start(ctx)
	defer func() { mgr.Stop(ctx); bus.Close() }()

	response, err := adapter.handler(InboundMessage{
		Text: "clear", Tenant: "t", Agent: "a",
	})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	// When clear fails, it returns a failure message (not an error)
	if !strings.Contains(response, "Failed to clear session") {
		t.Fatalf("expected failure message, got %q", response)
	}
}

func TestManagerChannelDetails(t *testing.T) {
	_, _, bus := setupManagerWithMocks(t)

	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "test", Target: "channel", Topic: "channel.details",
	})
	if err != nil {
		t.Fatalf("details: %v", err)
	}
	details, ok := resp.Payload.([]map[string]string)
	if !ok {
		t.Fatalf("expected []map[string]string, got %T", resp.Payload)
	}
	if len(details) != 1 {
		t.Fatalf("expected 1 adapter detail, got %d", len(details))
	}
	if details[0]["name"] != "test-channel" {
		t.Fatalf("expected adapter name 'test-channel', got %q", details[0]["name"])
	}
	if details[0]["status"] != "active" {
		t.Fatalf("expected status 'active', got %q", details[0]["status"])
	}
}

func TestManagerSendToUnknownChannel(t *testing.T) {
	_, _, bus := setupManagerWithMocks(t)

	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := bus.Request(reqCtx, ipc.Message{
		Source: "test", Target: "channel", Topic: "channel.send",
		Payload: OutboundMessage{Channel: "nonexistent", ChannelID: "C123", Text: "hello"},
	})
	if err == nil {
		t.Fatal("expected error when sending to unknown channel")
	}
}

func TestManagerAddMultipleAdapters(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	mgr := NewManager()
	mgr.AddAdapter(&mockAdapter{name: "slack"})
	mgr.AddAdapter(&mockAdapter{name: "teams"})
	mgr.AddAdapter(&mockAdapter{name: "webhook"})

	ctx := context.Background()
	mgr.Init(ctx, &kernel.Services{Bus: bus})
	mgr.Start(ctx)
	defer mgr.Stop(ctx)

	h := mgr.Health(ctx)
	if !strings.Contains(h.Message, "3 channel adapters") {
		t.Fatalf("expected health message with 3 adapters, got %q", h.Message)
	}
}
