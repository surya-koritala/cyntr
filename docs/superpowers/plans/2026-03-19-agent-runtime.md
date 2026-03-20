# Agent Runtime Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build Cyntr's own agent execution engine — model-agnostic LLM chat interface, tool execution with policy checks, session/context management, and multi-agent support per tenant.

**Architecture:** The agent runtime is a kernel module that manages agent instances per tenant. Each agent has a configured model provider, a set of allowed tools, and a session history. When an agent receives a message, it assembles context (system prompt + history + skills), calls the model provider, processes any tool calls through the policy engine, and returns the response. The `ModelProvider` interface abstracts LLM backends — this plan implements a mock provider for testing and a real Anthropic (Claude) adapter.

**Tech Stack:** Go 1.22+, `net/http` for API calls. No external LLM SDK deps — raw HTTP to keep it minimal.

**Spec:** `docs/superpowers/specs/2026-03-19-cyntr-enterprise-platform-design.md` (Section 2.3)

**Dependencies:** Kernel + Policy Engine + Tenant Manager + Auth (Plans 1-3).

**Deferred to future plans:**
- Additional model providers (GPT, Gemini, Ollama, Bedrock) — this plan builds the interface + Claude adapter. Others plug in identically.
- Browser tool, advanced file tools — this plan implements shell_exec and http_request tools.
- Skill injection per turn — comes with Plan 7 (Skill Runtime).
- Streaming responses — this plan uses non-streaming for simplicity. Streaming is an adapter-level enhancement.

---

## File Structure

```
modules/agent/
├── types.go               # Agent, AgentConfig, Message, ToolCall, ToolResult
├── provider.go            # ModelProvider interface
├── providers/
│   ├── mock.go            # MockProvider for testing (deterministic responses)
│   └── anthropic.go       # Claude API adapter
├── tools.go               # Tool interface, ToolRegistry
├── tools/
│   ├── shell.go           # ShellTool — executes commands (policy-gated)
│   └── http.go            # HTTPTool — makes HTTP requests (policy-gated)
├── session.go             # Session: history, context assembly
├── runtime.go             # Runtime kernel module: manages agents, handles IPC
├── types_test.go
├── provider_test.go       # MockProvider tests
├── tools_test.go          # Tool registry tests
├── session_test.go        # Session/context tests
└── runtime_test.go        # Runtime module IPC integration tests
```

---

## Chunk 1: Agent Types + Model Provider Interface

### Task 1: Define Agent Types

**Files:**
- Create: `modules/agent/types.go`
- Create: `modules/agent/types_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/agent/types_test.go`:
```go
package agent

import "testing"

func TestMessageRoleString(t *testing.T) {
	tests := []struct {
		r    Role
		want string
	}{
		{RoleUser, "user"},
		{RoleAssistant, "assistant"},
		{RoleSystem, "system"},
		{RoleTool, "tool"},
		{Role(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.r.String(); got != tt.want {
			t.Errorf("Role(%d).String() = %q, want %q", int(tt.r), got, tt.want)
		}
	}
}

func TestAgentConfigDefaults(t *testing.T) {
	cfg := AgentConfig{
		Name:   "test-agent",
		Tenant: "finance",
		Model:  "claude",
	}
	if cfg.Name != "test-agent" {
		t.Fatalf("expected test-agent, got %q", cfg.Name)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement types**

Create `modules/agent/types.go`:
```go
package agent

import "fmt"

// Role identifies the sender of a message in a conversation.
type Role int

const (
	RoleUser      Role = iota
	RoleAssistant
	RoleSystem
	RoleTool
)

func (r Role) String() string {
	switch r {
	case RoleUser:
		return "user"
	case RoleAssistant:
		return "assistant"
	case RoleSystem:
		return "system"
	case RoleTool:
		return "tool"
	default:
		return fmt.Sprintf("unknown(%d)", int(r))
	}
}

// Message represents a single message in a conversation.
type Message struct {
	Role      Role
	Content   string
	ToolCalls []ToolCall   // set when assistant requests tool use
	ToolResults []ToolResult // set when providing tool results
}

// ToolCall represents a request from the model to execute a tool.
type ToolCall struct {
	ID    string
	Name  string
	Input map[string]string
}

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}

// AgentConfig defines the configuration for a single agent instance.
type AgentConfig struct {
	Name         string   `yaml:"name"`
	Tenant       string   `yaml:"tenant"`
	Model        string   `yaml:"model"`          // provider name: "claude", "gpt", "mock"
	SystemPrompt string   `yaml:"system_prompt"`
	Tools        []string `yaml:"tools"`           // allowed tool names
	MaxTurns     int      `yaml:"max_turns"`       // max tool-use turns per request (default 10)
}

// ChatRequest is the IPC payload for agent.chat requests.
type ChatRequest struct {
	Agent   string // agent name
	Tenant  string // tenant name
	User    string // user making the request
	Message string // user's message
}

// ChatResponse is the IPC payload for agent.chat responses.
type ChatResponse struct {
	Agent   string
	Content string
	ToolsUsed []string // names of tools that were called
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/ -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add modules/agent/types.go modules/agent/types_test.go
git commit -m "feat(agent): define agent types — Message, ToolCall, AgentConfig, ChatRequest/Response"
```

---

### Task 2: Define ModelProvider Interface + Mock Provider

**Files:**
- Create: `modules/agent/provider.go`
- Create: `modules/agent/providers/mock.go`
- Create: `modules/agent/provider_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/agent/provider_test.go`:
```go
package agent

import (
	"context"
	"testing"

	"github.com/cyntr-dev/cyntr/modules/agent/providers"
)

func TestMockProviderChat(t *testing.T) {
	p := providers.NewMock("Hello from mock!")

	resp, err := p.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "Hi"},
	}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content != "Hello from mock!" {
		t.Fatalf("expected mock response, got %q", resp.Content)
	}
	if resp.Role != RoleAssistant {
		t.Fatalf("expected assistant role, got %s", resp.Role)
	}
}

func TestMockProviderWithToolCall(t *testing.T) {
	p := providers.NewMockWithToolCall("shell_exec", map[string]string{"command": "ls"})

	resp, err := p.Chat(context.Background(), []Message{
		{Role: RoleUser, Content: "List files"},
	}, []ToolDef{{Name: "shell_exec", Description: "Run shell command"}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "shell_exec" {
		t.Fatalf("expected shell_exec, got %q", resp.ToolCalls[0].Name)
	}
}

func TestMockProviderName(t *testing.T) {
	p := providers.NewMock("test")
	if p.Name() != "mock" {
		t.Fatalf("expected 'mock', got %q", p.Name())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Create provider interface**

Create `modules/agent/provider.go`:
```go
package agent

import "context"

// ModelProvider is the interface for LLM backends.
// Each provider (Claude, GPT, Gemini, etc.) implements this.
type ModelProvider interface {
	// Name returns the provider name (e.g., "claude", "gpt", "mock").
	Name() string

	// Chat sends messages to the model and returns a response.
	// tools defines which tools the model can request.
	// The response may contain ToolCalls if the model wants to use tools.
	Chat(ctx context.Context, messages []Message, tools []ToolDef) (Message, error)
}

// ToolDef describes a tool available to the model.
type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]ToolParam
}

// ToolParam describes a single parameter for a tool.
type ToolParam struct {
	Type        string
	Description string
	Required    bool
}
```

- [ ] **Step 4: Create mock provider**

Create `modules/agent/providers/mock.go`:
```go
package providers

import (
	"context"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// Mock is a deterministic model provider for testing.
type Mock struct {
	response  string
	toolCall  *agent.ToolCall
	callCount int
}

// NewMock creates a mock provider that always returns the given response.
func NewMock(response string) *Mock {
	return &Mock{response: response}
}

// NewMockWithToolCall creates a mock that returns a tool call on first request,
// then a text response on subsequent requests.
func NewMockWithToolCall(toolName string, input map[string]string) *Mock {
	return &Mock{
		response: "Tool result processed.",
		toolCall: &agent.ToolCall{
			ID:    "call_mock_001",
			Name:  toolName,
			Input: input,
		},
	}
}

func (m *Mock) Name() string { return "mock" }

func (m *Mock) Chat(ctx context.Context, messages []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	m.callCount++

	// If we have a tool call and this is the first request, return tool call
	if m.toolCall != nil && m.callCount == 1 {
		return agent.Message{
			Role:      agent.RoleAssistant,
			ToolCalls: []agent.ToolCall{*m.toolCall},
		}, nil
	}

	return agent.Message{
		Role:    agent.RoleAssistant,
		Content: m.response,
	}, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/... -v -count=1`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add modules/agent/provider.go modules/agent/providers/mock.go modules/agent/provider_test.go
git commit -m "feat(agent): define ModelProvider interface and mock provider for testing"
```

---

## Chunk 2: Tool System

### Task 3: Implement Tool Interface and Registry

**Files:**
- Create: `modules/agent/tools.go`
- Create: `modules/agent/tools_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/agent/tools_test.go`:
```go
package agent

import (
	"context"
	"testing"
)

type echoTool struct{}

func (t *echoTool) Name() string        { return "echo" }
func (t *echoTool) Description() string { return "Echoes input" }
func (t *echoTool) Parameters() map[string]ToolParam {
	return map[string]ToolParam{"text": {Type: "string", Description: "Text to echo", Required: true}}
}
func (t *echoTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	return "echo: " + input["text"], nil
}

func TestToolRegistryRegisterAndGet(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})

	tool, ok := reg.Get("echo")
	if !ok {
		t.Fatal("expected to find echo tool")
	}
	if tool.Name() != "echo" {
		t.Fatalf("expected echo, got %q", tool.Name())
	}
}

func TestToolRegistryGetNotFound(t *testing.T) {
	reg := NewToolRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestToolRegistryList(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})

	tools := reg.List()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0] != "echo" {
		t.Fatalf("expected echo, got %q", tools[0])
	}
}

func TestToolRegistryToolDefs(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})

	defs := reg.ToolDefs([]string{"echo"})
	if len(defs) != 1 {
		t.Fatalf("expected 1 def, got %d", len(defs))
	}
	if defs[0].Name != "echo" {
		t.Fatalf("expected echo, got %q", defs[0].Name)
	}
}

func TestToolRegistryExecute(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&echoTool{})

	result, err := reg.Execute(context.Background(), "echo", map[string]string{"text": "hello"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result != "echo: hello" {
		t.Fatalf("expected 'echo: hello', got %q", result)
	}
}

func TestToolRegistryExecuteNotFound(t *testing.T) {
	reg := NewToolRegistry()
	_, err := reg.Execute(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/ -run TestToolRegistry -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement tool system**

Create `modules/agent/tools.go`:
```go
package agent

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Tool is the interface for executable tools available to agents.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]ToolParam
	Execute(ctx context.Context, input map[string]string) (string, error)
}

// ToolRegistry manages available tools.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates an empty tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]Tool)}
}

// Register adds a tool to the registry.
func (r *ToolRegistry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

// Get returns a tool by name.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns all tool names sorted alphabetically.
func (r *ToolRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ToolDefs returns ToolDef descriptions for the given tool names.
// Unknown names are skipped.
func (r *ToolRegistry) ToolDefs(names []string) []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var defs []ToolDef
	for _, name := range names {
		t, ok := r.tools[name]
		if !ok {
			continue
		}
		defs = append(defs, ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	return defs
}

// Execute runs a tool by name with the given input.
func (r *ToolRegistry) Execute(ctx context.Context, name string, input map[string]string) (string, error) {
	r.mu.RLock()
	t, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("tool %q not found", name)
	}
	return t.Execute(ctx, input)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/agent/tools.go modules/agent/tools_test.go
git commit -m "feat(agent): implement Tool interface and ToolRegistry"
```

---

## Chunk 3: Session Management

### Task 4: Implement Session with History and Context Assembly

**Files:**
- Create: `modules/agent/session.go`
- Create: `modules/agent/session_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/agent/session_test.go`:
```go
package agent

import "testing"

func TestSessionAddAndGetHistory(t *testing.T) {
	s := NewSession("sess_001", AgentConfig{
		Name:         "test-agent",
		Tenant:       "finance",
		Model:        "mock",
		SystemPrompt: "You are a helpful assistant.",
		MaxTurns:     10,
	})

	s.AddMessage(Message{Role: RoleUser, Content: "Hello"})
	s.AddMessage(Message{Role: RoleAssistant, Content: "Hi there!"})

	history := s.History()
	if len(history) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(history))
	}
	if history[0].Role != RoleUser {
		t.Fatalf("expected user, got %s", history[0].Role)
	}
}

func TestSessionContextAssembly(t *testing.T) {
	s := NewSession("sess_001", AgentConfig{
		Name:         "test-agent",
		Tenant:       "finance",
		Model:        "mock",
		SystemPrompt: "You are a helpful assistant.",
	})

	s.AddMessage(Message{Role: RoleUser, Content: "Hello"})

	ctx := s.AssembleContext()
	// System prompt should be first
	if len(ctx) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(ctx))
	}
	if ctx[0].Role != RoleSystem {
		t.Fatalf("expected system first, got %s", ctx[0].Role)
	}
	if ctx[0].Content != "You are a helpful assistant." {
		t.Fatalf("expected system prompt, got %q", ctx[0].Content)
	}
	if ctx[1].Role != RoleUser {
		t.Fatalf("expected user second, got %s", ctx[1].Role)
	}
}

func TestSessionNoSystemPrompt(t *testing.T) {
	s := NewSession("sess_001", AgentConfig{
		Name:   "test-agent",
		Tenant: "finance",
		Model:  "mock",
	})

	s.AddMessage(Message{Role: RoleUser, Content: "Hello"})

	ctx := s.AssembleContext()
	if len(ctx) != 1 {
		t.Fatalf("expected 1 message (no system prompt), got %d", len(ctx))
	}
}

func TestSessionID(t *testing.T) {
	s := NewSession("sess_abc", AgentConfig{Name: "test"})
	if s.ID() != "sess_abc" {
		t.Fatalf("expected sess_abc, got %q", s.ID())
	}
}

func TestSessionConfig(t *testing.T) {
	cfg := AgentConfig{Name: "test", Tenant: "finance", Model: "claude", MaxTurns: 5}
	s := NewSession("sess_001", cfg)
	if s.Config().MaxTurns != 5 {
		t.Fatalf("expected 5, got %d", s.Config().MaxTurns)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/ -run TestSession -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement session**

Create `modules/agent/session.go`:
```go
package agent

import "sync"

// Session manages conversation history and context for a single agent interaction.
type Session struct {
	mu      sync.RWMutex
	id      string
	config  AgentConfig
	history []Message
}

// NewSession creates a new conversation session.
func NewSession(id string, config AgentConfig) *Session {
	if config.MaxTurns == 0 {
		config.MaxTurns = 10
	}
	return &Session{
		id:      id,
		config:  config,
		history: make([]Message, 0),
	}
}

// ID returns the session identifier.
func (s *Session) ID() string { return s.id }

// Config returns the agent configuration for this session.
func (s *Session) Config() AgentConfig {
	return s.config
}

// AddMessage appends a message to the conversation history.
func (s *Session) AddMessage(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, msg)
}

// History returns a copy of the conversation history.
func (s *Session) History() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := make([]Message, len(s.history))
	copy(h, s.history)
	return h
}

// AssembleContext builds the full message list for a model call:
// system prompt (if set) + conversation history.
func (s *Session) AssembleContext() []Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var ctx []Message

	if s.config.SystemPrompt != "" {
		ctx = append(ctx, Message{
			Role:    RoleSystem,
			Content: s.config.SystemPrompt,
		})
	}

	ctx = append(ctx, s.history...)
	return ctx
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/agent/session.go modules/agent/session_test.go
git commit -m "feat(agent): implement Session with history and context assembly"
```

---

## Chunk 4: Agent Runtime Module

### Task 5: Implement Runtime as Kernel Module

**Files:**
- Create: `modules/agent/runtime.go`
- Create: `modules/agent/runtime_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/agent/runtime_test.go`:
```go
package agent

import (
	"context"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent/providers"
)

func TestRuntimeImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Runtime)(nil)
}

func TestRuntimeCreateAgentAndChat(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	mockProvider := providers.NewMock("Hello from the agent!")
	toolReg := NewToolRegistry()

	rt := NewRuntime()
	rt.RegisterProvider(mockProvider)
	rt.SetToolRegistry(toolReg)

	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	// Create an agent
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: AgentConfig{
			Name: "test-agent", Tenant: "finance", Model: "mock",
			SystemPrompt: "You are helpful.", Tools: []string{}, MaxTurns: 10,
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("expected 'ok', got %v", resp.Payload)
	}

	// Chat with the agent
	resp, err = bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
		Payload: ChatRequest{Agent: "test-agent", Tenant: "finance", User: "jane@corp.com", Message: "Hi"},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	chatResp, ok := resp.Payload.(ChatResponse)
	if !ok {
		t.Fatalf("expected ChatResponse, got %T", resp.Payload)
	}
	if chatResp.Content != "Hello from the agent!" {
		t.Fatalf("expected mock response, got %q", chatResp.Content)
	}
}

func TestRuntimeChatWithToolUse(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	mockProvider := providers.NewMockWithToolCall("echo", map[string]string{"text": "test"})
	toolReg := NewToolRegistry()
	toolReg.Register(&echoToolImpl{})

	rt := NewRuntime()
	rt.RegisterProvider(mockProvider)
	rt.SetToolRegistry(toolReg)

	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Create agent with echo tool
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: AgentConfig{
			Name: "tool-agent", Tenant: "finance", Model: "mock",
			Tools: []string{"echo"}, MaxTurns: 10,
		},
	})

	// Chat — should trigger tool call then final response
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
		Payload: ChatRequest{Agent: "tool-agent", Tenant: "finance", User: "jane@corp.com", Message: "Echo something"},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	chatResp := resp.Payload.(ChatResponse)
	if chatResp.Content != "Tool result processed." {
		t.Fatalf("expected final response, got %q", chatResp.Content)
	}
	if len(chatResp.ToolsUsed) != 1 || chatResp.ToolsUsed[0] != "echo" {
		t.Fatalf("expected [echo], got %v", chatResp.ToolsUsed)
	}
}

func TestRuntimeChatAgentNotFound(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	rt := NewRuntime()
	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
		Payload: ChatRequest{Agent: "nonexistent", Tenant: "finance", Message: "Hi"},
	})
	if err == nil {
		t.Fatal("expected error for nonexistent agent")
	}
}

func TestRuntimeListAgents(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	rt := NewRuntime()
	rt.RegisterProvider(providers.NewMock("test"))

	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Create two agents
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: AgentConfig{Name: "agent-a", Tenant: "finance", Model: "mock"},
	})
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: AgentConfig{Name: "agent-b", Tenant: "finance", Model: "mock"},
	})

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.list",
		Payload: "finance",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	names, ok := resp.Payload.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", resp.Payload)
	}
	if len(names) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(names))
	}
}

func TestRuntimeHealthy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	rt := NewRuntime()
	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	health := rt.Health(ctx)
	if !health.Healthy {
		t.Fatalf("expected healthy: %s", health.Message)
	}
}

// echoToolImpl for testing
type echoToolImpl struct{}

func (t *echoToolImpl) Name() string        { return "echo" }
func (t *echoToolImpl) Description() string { return "Echoes input" }
func (t *echoToolImpl) Parameters() map[string]ToolParam {
	return map[string]ToolParam{"text": {Type: "string", Description: "Text to echo", Required: true}}
}
func (t *echoToolImpl) Execute(ctx context.Context, input map[string]string) (string, error) {
	return "echo: " + input["text"], nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/ -run TestRuntime -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement runtime module**

Create `modules/agent/runtime.go`:
```go
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Runtime is the Agent Runtime kernel module.
// It manages agent instances and orchestrates model calls + tool execution.
type Runtime struct {
	mu        sync.RWMutex
	bus       *ipc.Bus
	providers map[string]ModelProvider
	toolReg   *ToolRegistry
	agents    map[string]*agentInstance // "tenant/name" -> instance
}

type agentInstance struct {
	config  AgentConfig
	session *Session
}

// NewRuntime creates a new Agent Runtime module.
func NewRuntime() *Runtime {
	return &Runtime{
		providers: make(map[string]ModelProvider),
		agents:    make(map[string]*agentInstance),
	}
}

// RegisterProvider adds a model provider.
func (r *Runtime) RegisterProvider(p ModelProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

// SetToolRegistry sets the tool registry for all agents.
func (r *Runtime) SetToolRegistry(reg *ToolRegistry) {
	r.toolReg = reg
}

func (r *Runtime) Name() string           { return "agent_runtime" }
func (r *Runtime) Dependencies() []string { return nil }

func (r *Runtime) Init(ctx context.Context, svc *kernel.Services) error {
	r.bus = svc.Bus
	return nil
}

func (r *Runtime) Start(ctx context.Context) error {
	r.bus.Handle("agent_runtime", "agent.create", r.handleCreate)
	r.bus.Handle("agent_runtime", "agent.chat", r.handleChat)
	r.bus.Handle("agent_runtime", "agent.list", r.handleList)
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error { return nil }

func (r *Runtime) Health(ctx context.Context) kernel.HealthStatus {
	r.mu.RLock()
	count := len(r.agents)
	r.mu.RUnlock()
	return kernel.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("%d agents running", count),
	}
}

func (r *Runtime) handleCreate(msg ipc.Message) (ipc.Message, error) {
	cfg, ok := msg.Payload.(AgentConfig)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected AgentConfig, got %T", msg.Payload)
	}

	key := cfg.Tenant + "/" + cfg.Name
	sessID := "sess_" + generateShortID()

	r.mu.Lock()
	r.agents[key] = &agentInstance{
		config:  cfg,
		session: NewSession(sessID, cfg),
	}
	r.mu.Unlock()

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (r *Runtime) handleChat(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(ChatRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected ChatRequest, got %T", msg.Payload)
	}

	key := req.Tenant + "/" + req.Agent
	r.mu.RLock()
	inst, exists := r.agents[key]
	r.mu.RUnlock()

	if !exists {
		return ipc.Message{}, fmt.Errorf("agent %q not found in tenant %q", req.Agent, req.Tenant)
	}

	// Get the model provider
	r.mu.RLock()
	provider, ok := r.providers[inst.config.Model]
	r.mu.RUnlock()
	if !ok {
		return ipc.Message{}, fmt.Errorf("model provider %q not found", inst.config.Model)
	}

	// Add user message to session
	inst.session.AddMessage(Message{Role: RoleUser, Content: req.Message})

	// Get tool definitions for this agent
	var toolDefs []ToolDef
	if r.toolReg != nil && len(inst.config.Tools) > 0 {
		toolDefs = r.toolReg.ToolDefs(inst.config.Tools)
	}

	var toolsUsed []string

	// Agentic loop: call model, execute tools, repeat until no more tool calls
	for turn := 0; turn < inst.config.MaxTurns; turn++ {
		ctx := msg.Deadline
		_ = ctx

		response, err := provider.Chat(context.Background(), inst.session.AssembleContext(), toolDefs)
		if err != nil {
			return ipc.Message{}, fmt.Errorf("model call failed: %w", err)
		}

		inst.session.AddMessage(response)

		// If no tool calls, we're done
		if len(response.ToolCalls) == 0 {
			return ipc.Message{
				Type: ipc.MessageTypeResponse,
				Payload: ChatResponse{
					Agent:     req.Agent,
					Content:   response.Content,
					ToolsUsed: toolsUsed,
				},
			}, nil
		}

		// Execute tool calls
		for _, tc := range response.ToolCalls {
			toolsUsed = append(toolsUsed, tc.Name)

			var result string
			var isError bool

			if r.toolReg == nil {
				result = "tool registry not available"
				isError = true
			} else {
				var err error
				result, err = r.toolReg.Execute(context.Background(), tc.Name, tc.Input)
				if err != nil {
					result = err.Error()
					isError = true
				}
			}

			inst.session.AddMessage(Message{
				Role: RoleTool,
				ToolResults: []ToolResult{{
					CallID:  tc.ID,
					Content: result,
					IsError: isError,
				}},
			})
		}
	}

	return ipc.Message{}, fmt.Errorf("max turns (%d) exceeded", inst.config.MaxTurns)
}

func (r *Runtime) handleList(msg ipc.Message) (ipc.Message, error) {
	tenantFilter, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected tenant string, got %T", msg.Payload)
	}

	r.mu.RLock()
	var names []string
	for key, inst := range r.agents {
		if inst.config.Tenant == tenantFilter {
			names = append(names, inst.config.Name)
			_ = key
		}
	}
	r.mu.RUnlock()

	sort.Strings(names)

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: names}, nil
}

func generateShortID() string {
	buf := make([]byte, 4)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/agent/... -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/agent/runtime.go modules/agent/runtime_test.go
git commit -m "feat(agent): implement Agent Runtime kernel module with create, chat, and tool execution loop"
```

---

## Chunk 5: Integration Test + Final Verification

### Task 6: Agent Runtime Integration Test with Policy

**Files:**
- Create: `tests/integration/agent_runtime_test.go`

- [ ] **Step 1: Write integration test**

Create `tests/integration/agent_runtime_test.go`:
```go
package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/agent/providers"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

func TestAgentRuntimeWithPolicyAndTools(t *testing.T) {
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
	if err := k.LoadConfig(cfgPath); err != nil {
		t.Fatalf("config: %v", err)
	}

	// Set up modules
	policyEngine := policy.NewEngine(policyPath)
	agentRuntime := agent.NewRuntime()
	agentRuntime.RegisterProvider(providers.NewMock("I processed your request successfully."))

	if err := k.Register(policyEngine); err != nil {
		t.Fatalf("register policy: %v", err)
	}
	if err := k.Register(agentRuntime); err != nil {
		t.Fatalf("register agent: %v", err)
	}

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer k.Stop(ctx)

	bus := k.Bus()
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Create an agent
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: "assistant", Tenant: "marketing", Model: "mock",
			SystemPrompt: "You are a marketing assistant.",
			MaxTurns: 5,
		},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("expected ok, got %v", resp.Payload)
	}

	// Check policy first (like proxy would)
	policyResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "policy", Topic: "policy.check",
		Payload: policy.CheckRequest{
			Tenant: "marketing", Action: "model_call", Tool: "claude",
			Agent: "assistant", User: "bob@corp.com",
		},
	})
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	checkResp := policyResp.Payload.(policy.CheckResponse)
	if checkResp.Decision != policy.Allow {
		t.Fatalf("expected allow, got %s", checkResp.Decision)
	}

	// Chat with the agent
	chatResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent: "assistant", Tenant: "marketing",
			User: "bob@corp.com", Message: "Help me write a press release",
		},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	result := chatResp.Payload.(agent.ChatResponse)
	if result.Content != "I processed your request successfully." {
		t.Fatalf("unexpected response: %q", result.Content)
	}
	if result.Agent != "assistant" {
		t.Fatalf("expected agent 'assistant', got %q", result.Agent)
	}

	// List agents
	listResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.list",
		Payload: "marketing",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	agents := listResp.Payload.([]string)
	if len(agents) != 1 || agents[0] != "assistant" {
		t.Fatalf("expected [assistant], got %v", agents)
	}
}
```

- [ ] **Step 2: Run integration test**

Run: `cd /Users/suryakoritala/Cyntr && go test ./tests/integration/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 3: Commit**

```bash
git add tests/integration/agent_runtime_test.go
git commit -m "feat: add integration test — agent runtime with policy check and model chat"
```

---

### Task 7: Final Verification

- [ ] **Step 1: Run complete test suite**

Run: `cd /Users/suryakoritala/Cyntr && go test ./... -count=1 -race`
Expected: All PASS

- [ ] **Step 2: Run go vet**

Run: `cd /Users/suryakoritala/Cyntr && go vet ./...`

- [ ] **Step 3: Build binary**

Run: `cd /Users/suryakoritala/Cyntr && go build -o cyntr ./cmd/cyntr && ./cyntr version`
Expected: `cyntr v0.1.0`

- [ ] **Step 4: Verify clean git status**

Run: `git status`
Expected: Clean working tree
