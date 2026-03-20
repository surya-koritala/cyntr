package channel

import (
	"context"
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
