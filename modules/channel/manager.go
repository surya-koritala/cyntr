package channel

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

var logger = log.Default().WithModule("channel")

// Manager is the Channel Manager kernel module.
// It manages channel adapters and routes messages between channels and agents.
type Manager struct {
	mu       sync.RWMutex
	bus      *ipc.Bus
	adapters map[string]ChannelAdapter
}

// NewManager creates a new Channel Manager.
func NewManager() *Manager {
	return &Manager{
		adapters: make(map[string]ChannelAdapter),
	}
}

// AddAdapter registers a channel adapter.
func (m *Manager) AddAdapter(adapter ChannelAdapter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adapters[adapter.Name()] = adapter
}

func (m *Manager) Name() string           { return "channel" }
func (m *Manager) Dependencies() []string { return []string{"agent_runtime"} }

func (m *Manager) Init(ctx context.Context, svc *kernel.Services) error {
	m.bus = svc.Bus
	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	// Register IPC handlers
	m.bus.Handle("channel", "channel.send", m.handleSend)
	m.bus.Handle("channel", "channel.list", m.handleList)
	m.bus.Handle("channel", "channel.details", m.handleDetails)

	// Subscribe to progress events from agent runtime
	m.bus.Subscribe("channel", "agent.progress", m.handleProgress)

	// Start all adapters with the routing handler
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, adapter := range m.adapters {
		if err := adapter.Start(ctx, m.routeInbound); err != nil {
			return fmt.Errorf("start adapter %q: %w", adapter.Name(), err)
		}
	}

	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var firstErr error
	for _, adapter := range m.adapters {
		if err := adapter.Stop(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *Manager) Health(ctx context.Context) kernel.HealthStatus {
	m.mu.RLock()
	count := len(m.adapters)
	m.mu.RUnlock()

	return kernel.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("%d channel adapters", count),
	}
}

// routeInbound routes an inbound message to the appropriate agent via IPC.
func (m *Manager) routeInbound(msg InboundMessage) (string, error) {
	// Check for session control commands
	cleaned := strings.TrimSpace(strings.ToLower(msg.Text))
	if cleaned == "clear" || cleaned == "reset" || cleaned == "new conversation" || cleaned == "/clear" || cleaned == "/reset" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*1e9)
		defer cancel()
		_, err := m.bus.Request(ctx, ipc.Message{
			Source: "channel", Target: "agent_runtime", Topic: "agent.session.clear",
			Payload: map[string]string{"tenant": msg.Tenant, "name": msg.Agent},
		})
		if err != nil {
			return "Failed to clear session: " + err.Error(), nil
		}
		return "Session cleared. Starting fresh conversation.", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*1e9) // 5 minutes — agent may run multiple tool calls
	defer cancel()

	routeStart := time.Now()
	resp, err := m.bus.Request(ctx, ipc.Message{
		Source: "channel",
		Target: "agent_runtime",
		Topic:  "agent.chat",
		Payload: agent.ChatRequest{
			Agent:     msg.Agent,
			Tenant:    msg.Tenant,
			User:      msg.UserID,
			Message:   msg.Text,
			Channel:   msg.Channel,
			ChannelID: msg.ChannelID,
		},
	})
	routeDuration := time.Since(routeStart)
	if routeDuration > 5*time.Second {
		logger.Warn("slow agent response", map[string]any{
			"agent": msg.Agent, "channel": msg.Channel, "duration_ms": routeDuration.Milliseconds(),
		})
	}
	if err != nil {
		return "", fmt.Errorf("route to agent: %w", err)
	}

	chatResp, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		return "", fmt.Errorf("unexpected response type: %T", resp.Payload)
	}

	return agent.MaskSecrets(chatResp.Content), nil
}

func (m *Manager) handleSend(msg ipc.Message) (ipc.Message, error) {
	outMsg, ok := msg.Payload.(OutboundMessage)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected OutboundMessage, got %T", msg.Payload)
	}

	m.mu.RLock()
	adapter, ok := m.adapters[outMsg.Channel]
	m.mu.RUnlock()

	if !ok {
		return ipc.Message{}, fmt.Errorf("channel adapter %q not found", outMsg.Channel)
	}

	if err := adapter.Send(context.Background(), outMsg); err != nil {
		return ipc.Message{}, fmt.Errorf("send via %q: %w", outMsg.Channel, err)
	}

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (m *Manager) handleDetails(msg ipc.Message) (ipc.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	details := make([]map[string]string, 0, len(m.adapters))
	for name := range m.adapters {
		details = append(details, map[string]string{"name": name, "status": "active"})
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: details}, nil
}

func (m *Manager) handleList(msg ipc.Message) (ipc.Message, error) {
	m.mu.RLock()
	names := make([]string, 0, len(m.adapters))
	for name := range m.adapters {
		names = append(names, name)
	}
	m.mu.RUnlock()

	sort.Strings(names)

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: names}, nil
}

func (m *Manager) handleProgress(msg ipc.Message) (ipc.Message, error) {
	evt, ok := msg.Payload.(agent.ProgressEvent)
	if !ok {
		return ipc.Message{}, nil
	}
	if evt.Channel == "" || evt.ChannelID == "" {
		return ipc.Message{}, nil
	}

	m.mu.RLock()
	adapter, ok := m.adapters[evt.Channel]
	m.mu.RUnlock()

	if !ok {
		return ipc.Message{}, nil
	}

	if err := adapter.Send(context.Background(), OutboundMessage{
		Channel:   evt.Channel,
		ChannelID: evt.ChannelID,
		Text:      evt.Message,
	}); err != nil {
		logger.Warn("progress message failed", map[string]any{"channel": evt.Channel, "error": err.Error()})
	}
	return ipc.Message{}, nil
}
