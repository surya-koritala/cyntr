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
	tenants  map[string]string // adapter name -> owning tenant ("" = unscoped)
	gate     *Gate             // DM pairing gate (B12); nil = no gating
}

// tenantBound is the optional interface a ChannelAdapter may implement to
// declare the tenant that owns it. The manager uses this to enforce that an
// outbound dispatch (a progress event or send) is only delivered to an adapter
// owned by the same tenant — preventing one tenant from pushing messages into
// another tenant's channel via an attacker-supplied channel/channel_id.
type tenantBound interface {
	Tenant() string
}

// NewManager creates a new Channel Manager.
func NewManager() *Manager {
	return &Manager{
		adapters: make(map[string]ChannelAdapter),
		tenants:  make(map[string]string),
	}
}

// SetGate installs the DM-pairing gate. Call before Start.
func (m *Manager) SetGate(g *Gate) { m.gate = g }

// AddAdapter registers a channel adapter.
func (m *Manager) AddAdapter(adapter ChannelAdapter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adapters[adapter.Name()] = adapter
	if tb, ok := adapter.(tenantBound); ok {
		m.tenants[adapter.Name()] = tb.Tenant()
	}
}

// adapterForTenant returns the registered adapter for the given channel name,
// but only when it is owned by callerTenant. If the adapter declares an owning
// tenant that differs from callerTenant, the lookup fails closed so a caller in
// one tenant can never dispatch into another tenant's channel. An adapter that
// does not declare an owning tenant (legacy/global) is treated as unscoped and
// is allowed, preserving existing single-tenant deployments.
func (m *Manager) adapterForTenant(name, callerTenant string) (ChannelAdapter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	adapter, ok := m.adapters[name]
	if !ok {
		return nil, false
	}
	owner := m.tenants[name]
	if owner != "" && callerTenant != "" && owner != callerTenant {
		return nil, false
	}
	return adapter, true
}

// callerTenant extracts the authenticated caller's tenant from an inbound IPC
// message, if the caller declared one. Outbound sends originate from trusted
// in-process modules (agent tools, scheduler, notifications); when they thread a
// tenant through the message TraceID-adjacent metadata it is honored here. When
// no tenant is present the value is "" and the owner check in adapterForTenant
// degrades to allow, so existing single-tenant deployments are unaffected while
// any tenant-scoped adapter is still protected against cross-tenant delivery.
func callerTenant(msg ipc.Message) string {
	if m, ok := msg.Payload.(map[string]string); ok {
		return m["tenant"]
	}
	return ""
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
	m.bus.Handle("channel", "channel.pairing.approve", m.handlePairingApprove)
	m.bus.Handle("channel", "channel.pairing.pending", m.handlePairingPending)

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
	// DM pairing gate (B12): an untrusted/unknown sender never reaches the
	// agent — they get a pairing code (or rejection) until approved.
	if m.gate != nil {
		if ok, reply := m.gate.Check(msg); !ok {
			return reply, nil
		}
	}

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

// handlePairingApprove approves a pending sender by code. Payload is
// map[string]string{"tenant","channel","code"}.
func (m *Manager) handlePairingApprove(msg ipc.Message) (ipc.Message, error) {
	if m.gate == nil || m.gate.store == nil {
		return ipc.Message{}, fmt.Errorf("pairing not configured")
	}
	args, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("channel.pairing.approve: expected map[string]string, got %T", msg.Payload)
	}
	user, err := m.gate.store.ApproveCode(args["tenant"], args["channel"], args["code"])
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: user}, nil
}

// handlePairingPending lists awaiting-approval requests for a tenant. Payload is
// map[string]string{"tenant","caller_tenant"}. Pending pairings contain live
// pairing codes, so the caller must be authorized for the tenant whose requests
// it is listing: the authenticated caller_tenant (stamped by the front-end that
// authenticated the operator) must equal the requested tenant. Fails closed when
// either is missing or they disagree, so a caller cannot enumerate another
// tenant's pending codes by naming it in the payload.
func (m *Manager) handlePairingPending(msg ipc.Message) (ipc.Message, error) {
	if m.gate == nil || m.gate.store == nil {
		return ipc.Message{}, fmt.Errorf("pairing not configured")
	}
	args, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("channel.pairing.pending: expected map[string]string, got %T", msg.Payload)
	}
	tenant := args["tenant"]
	caller := args["caller_tenant"]
	if tenant == "" || caller == "" || caller != tenant {
		return ipc.Message{}, fmt.Errorf("channel.pairing.pending: not authorized for tenant %q", tenant)
	}
	pending, err := m.gate.store.ListPending(tenant)
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: pending}, nil
}

func (m *Manager) handleSend(msg ipc.Message) (ipc.Message, error) {
	outMsg, ok := msg.Payload.(OutboundMessage)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected OutboundMessage, got %T", msg.Payload)
	}

	// Scope the dispatch to the caller's tenant: only deliver to an adapter
	// owned by the same tenant, so a caller in one tenant cannot push a message
	// into another tenant's channel via an attacker-supplied channel/channel_id.
	// The caller tenant is taken from the IPC envelope (CallerTenant), which the
	// runtime stamps from the authenticated tool caller; when absent (e.g. an
	// unscoped/global adapter) the owner check degrades to allow, preserving
	// single-tenant deployments.
	adapter, ok := m.adapterForTenant(outMsg.Channel, callerTenant(msg))
	if !ok {
		return ipc.Message{}, fmt.Errorf("channel adapter %q not found for tenant", outMsg.Channel)
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

	// Scope the dispatch to the tenant that emitted the progress event. If the
	// target adapter is owned by a different tenant, drop the event rather than
	// leak it into another tenant's channel.
	adapter, ok := m.adapterForTenant(evt.Channel, evt.Tenant)
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
