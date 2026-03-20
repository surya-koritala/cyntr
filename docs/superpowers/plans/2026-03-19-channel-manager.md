# Channel Manager Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Channel Manager — a kernel module that abstracts messaging platform integrations behind a common `ChannelAdapter` interface, routes inbound messages to the correct agent, and sends responses back through the originating channel.

**Architecture:** The Channel Manager is a kernel module that manages channel adapters. Each adapter (Slack, Teams, webhook, etc.) implements a common interface for sending/receiving messages. Inbound messages are routed through auth resolution and policy checks before reaching the agent runtime. This plan implements the adapter interface, a webhook adapter (for testing and simple integrations), and the routing pipeline. Real platform SDKs (Slack API, Teams Bot Framework) are deferred — they follow the same interface.

**Tech Stack:** Go 1.22+ stdlib `net/http`. No external messaging SDKs in this plan.

**Spec:** `docs/superpowers/specs/2026-03-19-cyntr-enterprise-platform-design.md` (Section 2.4)

**Dependencies:** Kernel + Auth + Agent Runtime (Plans 1-4).

**Deferred to future plans:**
- Slack, Microsoft Teams, Google Chat, Email adapters — need external SDKs and live API credentials. This plan builds the interface and a webhook adapter they'll plug into.
- WebSocket-based adapters — this plan uses HTTP webhooks.
- Channel identity binding to enterprise IdP — the auth module is called, but full IdP integration is deferred.

---

## File Structure

```
modules/channel/
├── types.go               # ChannelAdapter interface, InboundMessage, OutboundMessage
├── webhook/
│   └── adapter.go         # Webhook adapter: HTTP endpoint for inbound, HTTP POST for outbound
├── manager.go             # ChannelManager kernel module: routes messages, manages adapters
├── types_test.go
├── webhook/
│   └── adapter_test.go
└── manager_test.go        # Module IPC + routing tests
```

---

## Chunk 1: Types + Webhook Adapter

### Task 1: Define Channel Types

**Files:**
- Create: `modules/channel/types.go`
- Create: `modules/channel/types_test.go`

- [ ] **Step 1: Write failing test**

Create `modules/channel/types_test.go`:
```go
package channel

import "testing"

func TestInboundMessageFields(t *testing.T) {
	msg := InboundMessage{
		Channel:   "slack",
		ChannelID: "C1234",
		UserID:    "U5678",
		Text:      "Hello agent",
		Tenant:    "marketing",
		Agent:     "assistant",
	}
	if msg.Channel != "slack" {
		t.Fatalf("expected slack, got %q", msg.Channel)
	}
	if msg.Text != "Hello agent" {
		t.Fatalf("expected message text, got %q", msg.Text)
	}
}

func TestOutboundMessageFields(t *testing.T) {
	msg := OutboundMessage{
		Channel:   "slack",
		ChannelID: "C1234",
		Text:      "Hello user!",
	}
	if msg.Text != "Hello user!" {
		t.Fatalf("expected text, got %q", msg.Text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/channel/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement types**

Create `modules/channel/types.go`:
```go
package channel

import "context"

// ChannelAdapter is the interface for messaging platform integrations.
// Each platform (Slack, Teams, webhook, etc.) implements this.
type ChannelAdapter interface {
	// Name returns the adapter name (e.g., "slack", "teams", "webhook").
	Name() string

	// Start begins listening for inbound messages.
	// The handler is called for each inbound message.
	Start(ctx context.Context, handler InboundHandler) error

	// Stop shuts down the adapter.
	Stop(ctx context.Context) error

	// Send sends a message through this channel.
	Send(ctx context.Context, msg OutboundMessage) error
}

// InboundHandler is called when a message arrives from a channel.
type InboundHandler func(msg InboundMessage) (string, error)

// InboundMessage represents a message received from a messaging platform.
type InboundMessage struct {
	Channel   string // adapter name: "slack", "teams", "webhook"
	ChannelID string // platform-specific channel/room ID
	UserID    string // platform-specific user ID
	Text      string // message content
	Tenant    string // resolved tenant
	Agent     string // target agent name
}

// OutboundMessage represents a message to send through a channel.
type OutboundMessage struct {
	Channel   string // adapter name
	ChannelID string // platform-specific channel/room ID
	Text      string // message content
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/channel/ -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add modules/channel/types.go modules/channel/types_test.go
git commit -m "feat(channel): define ChannelAdapter interface and message types"
```

---

### Task 2: Implement Webhook Adapter

**Files:**
- Create: `modules/channel/webhook/adapter.go`
- Create: `modules/channel/webhook/adapter_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/channel/webhook/adapter_test.go`:
```go
package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

func TestWebhookAdapterImplementsInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestWebhookAdapterReceiveMessage(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)

	adapter := New("127.0.0.1:0")
	ctx := context.Background()

	err := adapter.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "Got it!", nil
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer adapter.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	// Send a webhook
	body := `{"tenant":"marketing","agent":"assistant","user_id":"U123","channel_id":"C456","text":"Hello"}`
	resp, err := http.Post("http://"+adapter.Addr()+"/webhook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["response"] != "Got it!" {
		t.Fatalf("expected 'Got it!', got %v", result)
	}

	select {
	case msg := <-received:
		if msg.Text != "Hello" {
			t.Fatalf("expected 'Hello', got %q", msg.Text)
		}
		if msg.Tenant != "marketing" {
			t.Fatalf("expected marketing, got %q", msg.Tenant)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestWebhookAdapterSend(t *testing.T) {
	// Create a test server to receive outbound messages
	var sentBody map[string]string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentBody)
		w.WriteHeader(200)
	}))
	defer target.Close()

	adapter := New("127.0.0.1:0")
	adapter.SetOutboundURL(target.URL)

	ctx := context.Background()
	adapter.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer adapter.Stop(ctx)

	err := adapter.Send(ctx, channel.OutboundMessage{
		Channel:   "webhook",
		ChannelID: "C456",
		Text:      "Hello from agent!",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if sentBody["text"] != "Hello from agent!" {
		t.Fatalf("expected message, got %v", sentBody)
	}
}

func TestWebhookAdapterName(t *testing.T) {
	a := New("127.0.0.1:0")
	if a.Name() != "webhook" {
		t.Fatalf("expected webhook, got %q", a.Name())
	}
}

func TestWebhookAdapterBadJSON(t *testing.T) {
	adapter := New("127.0.0.1:0")
	ctx := context.Background()
	adapter.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer adapter.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Post("http://"+adapter.Addr()+"/webhook", "application/json", strings.NewReader(`{bad json`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/channel/webhook/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement webhook adapter**

Create `modules/channel/webhook/adapter.go`:
```go
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

// Adapter is a webhook-based channel adapter.
// Receives messages via HTTP POST and sends responses via HTTP POST.
type Adapter struct {
	listenAddr  string
	outboundURL string
	listener    net.Listener
	server      *http.Server
	handler     channel.InboundHandler
}

// New creates a new webhook adapter.
func New(listenAddr string) *Adapter {
	return &Adapter{listenAddr: listenAddr}
}

func (a *Adapter) Name() string { return "webhook" }

// SetOutboundURL sets the URL for outbound message delivery.
func (a *Adapter) SetOutboundURL(url string) {
	a.outboundURL = url
}

// Addr returns the actual listening address.
func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", a.handleWebhook)

	ln, err := net.Listen("tcp", a.listenAddr)
	if err != nil {
		return fmt.Errorf("webhook listen: %w", err)
	}
	a.listener = ln
	a.server = &http.Server{Handler: mux}

	go a.server.Serve(ln)
	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {
	if a.server != nil {
		return a.server.Shutdown(ctx)
	}
	return nil
}

func (a *Adapter) Send(ctx context.Context, msg channel.OutboundMessage) error {
	if a.outboundURL == "" {
		return fmt.Errorf("no outbound URL configured")
	}

	payload := map[string]string{
		"channel_id": msg.ChannelID,
		"text":       msg.Text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	resp, err := http.Post(a.outboundURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("outbound webhook returned %d", resp.StatusCode)
	}

	return nil
}

type webhookPayload struct {
	Tenant    string `json:"tenant"`
	Agent     string `json:"agent"`
	UserID    string `json:"user_id"`
	ChannelID string `json:"channel_id"`
	Text      string `json:"text"`
}

func (a *Adapter) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload webhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	msg := channel.InboundMessage{
		Channel:   "webhook",
		ChannelID: payload.ChannelID,
		UserID:    payload.UserID,
		Text:      payload.Text,
		Tenant:    payload.Tenant,
		Agent:     payload.Agent,
	}

	response, err := a.handler(msg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"response": response})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/channel/... -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/channel/webhook/
git commit -m "feat(channel): implement webhook adapter with HTTP inbound/outbound"
```

---

## Chunk 2: Channel Manager Module

### Task 3: Implement Channel Manager as Kernel Module

**Files:**
- Create: `modules/channel/manager.go`
- Create: `modules/channel/manager_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/channel/manager_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/channel/ -run TestManager -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement channel manager**

Create `modules/channel/manager.go`:
```go
package channel

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

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
	ctx, cancel := context.WithTimeout(context.Background(), 30*1e9) // 30 seconds
	defer cancel()

	resp, err := m.bus.Request(ctx, ipc.Message{
		Source: "channel",
		Target: "agent_runtime",
		Topic:  "agent.chat",
		Payload: agent.ChatRequest{
			Agent:   msg.Agent,
			Tenant:  msg.Tenant,
			User:    msg.UserID,
			Message: msg.Text,
		},
	})
	if err != nil {
		return "", fmt.Errorf("route to agent: %w", err)
	}

	chatResp, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		return "", fmt.Errorf("unexpected response type: %T", resp.Payload)
	}

	return chatResp.Content, nil
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/channel/... -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/channel/manager.go modules/channel/manager_test.go
git commit -m "feat(channel): implement Channel Manager kernel module with routing and send via IPC"
```

---

## Chunk 3: Integration Test + Final Verification

### Task 4: Integration Test

**Files:**
- Create: `tests/integration/channel_test.go`

- [ ] **Step 1: Write integration test**

Create `tests/integration/channel_test.go`:
```go
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/agent/providers"
	"github.com/cyntr-dev/cyntr/modules/channel"
	"github.com/cyntr-dev/cyntr/modules/channel/webhook"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

func TestChannelWebhookEndToEnd(t *testing.T) {
	dir := t.TempDir()

	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte(`
rules:
  - name: allow-all
    tenant: "*"
    action: "*"
    tool: "*"
    agent: "*"
    decision: allow
    priority: 1
`), 0644)

	cfgPath := filepath.Join(dir, "cyntr.yaml")
	os.WriteFile(cfgPath, []byte("version: \"1\"\nlisten:\n  address: \"127.0.0.1:8080\"\n"), 0644)

	k := kernel.New()
	k.LoadConfig(cfgPath)

	// Modules
	policyEngine := policy.NewEngine(policyPath)
	agentRuntime := agent.NewRuntime()
	agentRuntime.RegisterProvider(providers.NewMock("Hello from Cyntr!"))

	webhookAdapter := webhook.New("127.0.0.1:0")
	channelMgr := channel.NewManager()
	channelMgr.AddAdapter(webhookAdapter)

	k.Register(policyEngine)
	k.Register(agentRuntime)
	k.Register(channelMgr)

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer k.Stop(ctx)

	// Create an agent
	bus := k.Bus()
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: "assistant", Tenant: "marketing", Model: "mock",
			SystemPrompt: "You are helpful.", MaxTurns: 5,
		},
	})

	time.Sleep(200 * time.Millisecond)

	// Send webhook message
	body := `{"tenant":"marketing","agent":"assistant","user_id":"U123","channel_id":"C456","text":"Hi there"}`
	resp, err := http.Post("http://"+webhookAdapter.Addr()+"/webhook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("webhook: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["response"] != "Hello from Cyntr!" {
		t.Fatalf("expected agent response, got %v", result)
	}
}
```

Note: This test needs `ipc` imported. Add the import: `"github.com/cyntr-dev/cyntr/kernel/ipc"`.

- [ ] **Step 2: Run integration test**

Run: `cd /Users/suryakoritala/Cyntr && go test ./tests/integration/ -run TestChannel -v -count=1 -race`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add tests/integration/channel_test.go
git commit -m "feat: add integration test — webhook channel to agent runtime end-to-end"
```

---

### Task 5: Final Verification

- [ ] **Step 1: Run complete test suite**

Run: `cd /Users/suryakoritala/Cyntr && go test ./... -count=1 -race`
Expected: All PASS

- [ ] **Step 2: Run go vet**

Run: `cd /Users/suryakoritala/Cyntr && go vet ./...`

- [ ] **Step 3: Build binary**

Run: `cd /Users/suryakoritala/Cyntr && go build -o cyntr ./cmd/cyntr && ./cyntr version`
Expected: `cyntr v0.1.0`
