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
	mu          sync.RWMutex
	bus         *ipc.Bus
	providers   map[string]ModelProvider
	toolReg     *ToolRegistry
	agents      map[string]*agentInstance // "tenant/name" -> instance
	store       *SessionStore
	memoryStore *MemoryStore
}

// SetSessionStore attaches a SessionStore to the runtime for persistent conversations.
func (r *Runtime) SetSessionStore(store *SessionStore) {
	r.store = store
}

// SetMemoryStore attaches a MemoryStore to the runtime for long-term memory persistence.
func (r *Runtime) SetMemoryStore(store *MemoryStore) {
	r.memoryStore = store
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

	session := NewSession(sessID, cfg)
	if r.store != nil {
		r.store.SaveSession(sessID, cfg)
		session.SetStore(r.store)
	}

	r.mu.Lock()
	r.agents[key] = &agentInstance{
		config:  cfg,
		session: session,
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

	// Inject long-term memories into session context before the agentic loop
	if r.memoryStore != nil {
		if memories, err := r.memoryStore.Recall(req.Agent, req.Tenant); err == nil {
			inst.session.SetMemories(FormatForContext(memories))
		}
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
				var execErr error
				result, execErr = r.toolReg.Execute(context.Background(), tc.Name, tc.Input)
				if execErr != nil {
					result = execErr.Error()
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
	for _, inst := range r.agents {
		if inst.config.Tenant == tenantFilter {
			names = append(names, inst.config.Name)
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
