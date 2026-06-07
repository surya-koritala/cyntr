package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/observability"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/modules/usermodel"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// tracer for the agent runtime — created once, cheap to reuse. When the
// observability module is in no-op mode this is a no-op tracer.
var runtimeTracer = observability.Tracer("github.com/cyntr-dev/cyntr/modules/agent")

// busRequestTraced wraps bus.Request in an `ipc.request` span so cross-module
// hops show up in trace timelines. Drop-in replacement for r.bus.Request that
// preserves error semantics. When tracing is disabled the span is a no-op and
// this costs only the closure overhead.
func (r *Runtime) busRequestTraced(ctx context.Context, msg ipc.Message) (ipc.Message, error) {
	ctx, span := runtimeTracer.Start(ctx, "ipc.request")
	span.SetAttributes(
		attribute.String("target", msg.Target),
		attribute.String("topic", msg.Topic),
	)
	defer span.End()
	resp, err := r.bus.Request(ctx, msg)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
	}
	return resp, err
}

var logger = log.Default().WithModule("agent_runtime")

// agentRateLimiter tracks request counts per minute for a single agent.
type agentRateLimiter struct {
	mu      sync.Mutex
	count   int
	resetAt time.Time
}

var rateLimiters = make(map[string]*agentRateLimiter)
var rateLimiterMu sync.Mutex

// checkAgentRateLimit enforces per-agent request rate limits.
// Returns an error if the agent has exceeded its configured requests/minute.
// A limit of 0 or less means unlimited.
func checkAgentRateLimit(key string, limit int) error {
	if limit <= 0 {
		return nil
	}
	rateLimiterMu.Lock()
	rl, ok := rateLimiters[key]
	if !ok {
		rl = &agentRateLimiter{resetAt: time.Now().Add(time.Minute)}
		rateLimiters[key] = rl
	}
	rateLimiterMu.Unlock()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	if time.Now().After(rl.resetAt) {
		rl.count = 0
		rl.resetAt = time.Now().Add(time.Minute)
	}
	if rl.count >= limit {
		return fmt.Errorf("rate limit exceeded: %d requests/minute for agent", limit)
	}
	rl.count++
	return nil
}

// Runtime is the Agent Runtime kernel module.
// It manages agent instances and orchestrates model calls + tool execution.
type Runtime struct {
	mu               sync.RWMutex
	bus              *ipc.Bus
	providers        map[string]ModelProvider
	toolReg          *ToolRegistry
	agents           map[string]*agentInstance // "tenant/name" -> instance
	store            *SessionStore
	memoryStore      *MemoryStore
	usageStore       *UsageStore
	contextLoader    *ContextLoader
	contextStore     *ContextStore
	personalityStore *PersonalityStore
}

// SetSessionStore attaches a SessionStore to the runtime for persistent conversations.
func (r *Runtime) SetSessionStore(store *SessionStore) {
	r.store = store
}

// SetMemoryStore attaches a MemoryStore to the runtime for long-term memory persistence.
func (r *Runtime) SetMemoryStore(store *MemoryStore) {
	r.memoryStore = store
}

// SetUsageStore attaches a UsageStore to the runtime for token/cost tracking.
func (r *Runtime) SetUsageStore(store *UsageStore) {
	r.usageStore = store
}

// SetContextLoader attaches a per-workspace context-file loader (A7) whose
// content is prepended to every chat's system context.
func (r *Runtime) SetContextLoader(cl *ContextLoader) {
	r.contextLoader = cl
}

// SetContextStore attaches the shared-context store that backs stateful
// subagent coordination (#48).
func (r *Runtime) SetContextStore(cs *ContextStore) {
	r.contextStore = cs
}

// SetPersonalityStore attaches the switchable-personality catalog (F24). The
// selected persona's prompt is prepended to each chat's system context.
func (r *Runtime) SetPersonalityStore(ps *PersonalityStore) {
	r.personalityStore = ps
}

type agentInstance struct {
	config  AgentConfig
	session *Session
	// chatMu serializes a full chat turn for this (tenant, agent). The Session
	// is shared across concurrent chats, so without this two requests would
	// interleave their user/assistant/tool messages into one history and
	// clobber per-call state (lastUser, memories). Held for the whole agentic
	// loop so each chat sees a consistent, non-interleaved session.
	chatMu sync.Mutex
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

// Providers returns the names of all registered model providers.
func (r *Runtime) Providers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// Provider returns the registered ModelProvider with the given name, or nil
// if none. Exposed so external wiring (e.g. the usermodel distiller) can
// reuse the runtime's already-configured providers without re-instantiating.
func (r *Runtime) Provider(name string) ModelProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[name]
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
	r.bus.Handle("agent_runtime", "agent.get", r.handleGet)
	r.bus.Handle("agent_runtime", "agent.delete", r.handleDelete)
	r.bus.Handle("agent_runtime", "agent.sessions", r.handleSessions)
	r.bus.Handle("agent_runtime", "agent.session.messages", r.handleSessionMessages)
	r.bus.Handle("agent_runtime", "agent.memories", r.handleMemories)
	r.bus.Handle("agent_runtime", "agent.memory.save", r.handleMemorySave)
	r.bus.Handle("agent_runtime", "agent.memory.delete", r.handleMemoryDelete)
	r.bus.Handle("agent_runtime", "agent.session.clear", r.handleSessionClear)
	r.bus.Handle("agent_runtime", "agent.update", r.handleUpdate)
	r.bus.Handle("agent_runtime", "agent.search", r.handleSearch)
	r.bus.Handle("agent_runtime", "agent.versions", r.handleVersions)
	r.bus.Handle("agent_runtime", "agent.rollback", r.handleRollback)
	r.bus.Handle("agent_runtime", TopicContextWrite, r.handleContextWrite)
	r.bus.Handle("agent_runtime", TopicContextRead, r.handleContextRead)
	return r.LoadSavedAgents()
}

func (r *Runtime) Stop(ctx context.Context) error {
	// Close the shared-context store so its WAL is checkpointed cleanly on
	// shutdown (the other stores are owned/closed by main.go).
	if r.contextStore != nil {
		return r.contextStore.Close()
	}
	return nil
}

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

	if cfg.MaxTurns == 0 {
		cfg.MaxTurns = 10
	}

	key := cfg.Tenant + "/" + cfg.Name
	sessID := "sess_" + cfg.Tenant + "_" + cfg.Name

	session := NewSession(sessID, cfg)
	if r.store != nil {
		r.store.SaveSession(sessID, cfg)
		session.SetStore(r.store)
		r.store.SaveAgent(cfg)
		r.store.SaveAgentVersion(cfg)
	}

	r.mu.Lock()
	r.agents[key] = &agentInstance{
		config:  cfg,
		session: session,
	}
	r.mu.Unlock()

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

// LoadSavedAgents reads persisted agent configs from the store and recreates
// agent instances so they are available after a restart.
func (r *Runtime) LoadSavedAgents() error {
	if r.store == nil {
		return nil
	}
	agents, err := r.store.LoadAgents()
	if err != nil {
		return fmt.Errorf("load saved agents: %w", err)
	}
	for _, cfg := range agents {
		if cfg.MaxTurns == 0 {
			cfg.MaxTurns = 10
		}
		key := cfg.Tenant + "/" + cfg.Name
		r.mu.RLock()
		_, exists := r.agents[key]
		r.mu.RUnlock()
		if exists {
			continue
		}

		sessID := "sess_" + cfg.Tenant + "_" + cfg.Name
		session := NewSession(sessID, cfg)
		session.SetStore(r.store)

		// Try to restore messages from store
		_, messages, err := r.store.LoadSession(sessID)
		if err == nil {
			for _, msg := range messages {
				session.mu.Lock()
				session.history = append(session.history, msg)
				session.mu.Unlock()
			}
		}

		// Inject memories if memory store exists
		if r.memoryStore != nil {
			memories, _ := r.memoryStore.Recall(cfg.Name, cfg.Tenant)
			if len(memories) > 0 {
				session.SetMemories(FormatForContext(memories))
			}
		}

		r.mu.Lock()
		r.agents[key] = &agentInstance{
			config:  cfg,
			session: session,
		}
		r.mu.Unlock()
	}
	return nil
}

// loadUserProfile fetches the curated profile + preferences for (tenant, user)
// from the usermodel module via the IPC bus and renders them as system-prompt
// text. Returns "" when no profile is available, the module isn't registered,
// or anything else goes wrong — never errors, so a missing user model never
// breaks an in-flight chat.
func (r *Runtime) loadUserProfile(tenant, user string) string {
	if r.bus == nil || tenant == "" || user == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := r.busRequestTraced(ctx, ipc.Message{
		Source: "agent_runtime", Target: "usermodel", Topic: "usermodel.get",
		Payload: map[string]string{"tenant": tenant, "user": user},
	})
	if err != nil {
		// ipc.ErrNoHandler is expected when the usermodel module isn't
		// registered; treat as "no profile" and move on. Other errors are
		// also swallowed so a transient store failure doesn't block chat.
		return ""
	}

	p, ok := resp.Payload.(usermodel.UserProfile)
	if !ok {
		return ""
	}
	if p.ProfileMD == "" && p.PreferencesMD == "" {
		// Cold-start nudge: kick off an async first-time distill if there's
		// any recorded activity. The distiller itself will no-op if the
		// tenant hasn't opted in or activity is below the minimum, so this
		// is safe to fire unconditionally. We dispatch on a goroutine so
		// the chat path doesn't block on the LLM call.
		bus := r.bus
		go func() {
			asyncCtx, asyncCancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer asyncCancel()
			bus.Request(asyncCtx, ipc.Message{
				Source: "agent_runtime", Target: "usermodel", Topic: "usermodel.distill",
				Payload: map[string]string{"tenant": tenant, "user": user},
			})
		}()
		return ""
	}

	var b strings.Builder
	b.WriteString("User profile:\n")
	if p.ProfileMD == "" {
		b.WriteString("(none)")
	} else {
		b.WriteString(p.ProfileMD)
	}
	b.WriteString("\n\nUser preferences:\n")
	if p.PreferencesMD == "" {
		b.WriteString("(none)")
	} else {
		b.WriteString(p.PreferencesMD)
	}
	b.WriteString("\n")
	return b.String()
}

// checkToolPolicy checks if a tool execution is allowed.
// Returns "allow", "deny", or "require_approval".
// When no policy module is registered, defaults to "allow" (permissive).
func (r *Runtime) checkToolPolicy(tenant, user, agentName, toolName string) string {
	if r.bus == nil {
		return "allow"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := r.busRequestTraced(ctx, ipc.Message{
		Source: "agent_runtime", Target: "policy", Topic: "policy.check",
		Payload: policy.CheckRequest{
			Tenant: tenant, Action: "tool_call", Tool: toolName,
			Agent: agentName, User: user,
		},
	})
	if err != nil {
		// If no policy module is registered, allow by default.
		// Only fail-closed if the policy module is present but errored.
		if err == ipc.ErrNoHandler {
			return "allow"
		}
		return "deny" // policy module present but errored: fail-closed
	}

	checkResp, ok := resp.Payload.(policy.CheckResponse)
	if !ok {
		return "deny"
	}

	return checkResp.Decision.String()
}

func (r *Runtime) handleChat(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(ChatRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected ChatRequest, got %T", msg.Payload)
	}

	// A turn spawned by another agent (orchestrate/delegate) is a subagent
	// turn; its memory/recall/activity is scoped out of the shared user's
	// durable state so child-internal work can't contaminate the parent.
	isSubagent := msg.Source == "orchestrate" || msg.Source == "delegate_tool"

	// Top-level chat span. We use context.Background() because handleChat is
	// driven off the IPC bus rather than an inbound request context, but the
	// returned ctx is plumbed into downstream provider/tool calls so child
	// spans nest correctly.
	ctx, span := runtimeTracer.Start(context.Background(), "agent.chat") // Attributes are kept low-cardinality (no message body) so this is
	// safe to enable at 100% sampling in production.

	span.SetAttributes(
		attribute.String("tenant", req.Tenant),
		attribute.String("agent", req.Agent),
		attribute.String("user", req.User),
	)
	defer span.End()

	key := req.Tenant + "/" + req.Agent
	r.mu.RLock()
	inst, exists := r.agents[key]
	r.mu.RUnlock()

	if !exists {
		span.SetStatus(codes.Error, "agent not found")
		observability.RecordChatRequest(ctx, req.Tenant, req.Agent, "error")
		return ipc.Message{}, fmt.Errorf("agent %q not found in tenant %q", req.Agent, req.Tenant)
	}

	// '/personality <name>' (F24) is a control command, not a model turn —
	// intercept it before any provider/quota work and switch the session persona.
	if r.personalityStore != nil {
		if name, isCmd := ParsePersonalityCommand(req.Message); isCmd {
			sess := inst.session.ID()
			r.personalityStore.SeedDefaults(req.Tenant)
			if name == "" {
				names := r.personalityStore.PersonalityNames(req.Tenant)
				active := r.personalityStore.ActiveName(req.Tenant, sess)
				return ipc.Message{Type: ipc.MessageTypeResponse, Payload: ChatResponse{
					Agent:   req.Agent,
					Content: fmt.Sprintf("Active personality: %q. Available: %s", active, strings.Join(names, ", ")),
				}}, nil
			}
			p, err := r.personalityStore.Select(req.Tenant, sess, name)
			if err != nil {
				if errors.Is(err, ErrUnknownPersonality) {
					return ipc.Message{Type: ipc.MessageTypeResponse, Payload: ChatResponse{
						Agent:   req.Agent,
						Content: fmt.Sprintf("Unknown personality %q. Available: %s", name, strings.Join(r.personalityStore.PersonalityNames(req.Tenant), ", ")),
					}}, nil
				}
				return ipc.Message{}, err
			}
			return ipc.Message{Type: ipc.MessageTypeResponse, Payload: ChatResponse{
				Agent: req.Agent, Content: "Personality set to " + p.Name + ".",
			}}, nil
		}
	}

	// Enforce per-agent rate limit
	if err := checkAgentRateLimit(key, inst.config.RateLimit); err != nil {
		span.SetStatus(codes.Error, "rate limited")
		observability.RecordChatRequest(ctx, req.Tenant, req.Agent, "rate_limited")
		return ipc.Message{}, err
	}

	// Enforce per-tenant quotas via the quota module. When the quota module is
	// not registered (ipc.ErrNoHandler), every check is treated as "allowed" so
	// existing deployments behave unchanged.
	//
	// Order matters: rate first (cheap), then concurrency-slot acquire (carries
	// a release we must defer). We treat quota denials like denied policies —
	// returning a normal ChatResponse rather than an error so channel adapters
	// surface the message to the end user.
	if ok, reason := quotaCheck(r.bus, req.Tenant, "rate", 1); !ok {
		return ipc.Message{
			Type:    ipc.MessageTypeResponse,
			Payload: ChatResponse{Agent: req.Agent, Content: "quota exceeded: " + reason},
		}, nil
	}
	release, ok, reason := quotaAcquireSlot(r.bus, req.Tenant)
	if !ok {
		return ipc.Message{
			Type:    ipc.MessageTypeResponse,
			Payload: ChatResponse{Agent: req.Agent, Content: "quota exceeded: " + reason},
		}, nil
	}
	defer release()

	// Serialize the full turn for this agent's shared Session so concurrent
	// chats don't interleave their history or clobber lastUser/memories.
	inst.chatMu.Lock()
	defer inst.chatMu.Unlock()

	chatStart := time.Now()

	r.publishActivity(req.Agent, req.Tenant, "chat_start", "User: "+req.Message[:min(80, len(req.Message))])

	// Get the model provider
	r.mu.RLock()
	provider, ok := r.providers[inst.config.Model]
	r.mu.RUnlock()
	if !ok {
		return ipc.Message{}, fmt.Errorf("model provider %q not found", inst.config.Model)
	}

	// Build the system-context prelude: curated user profile (from the
	// usermodel module, if registered) followed by the flat long-term memory
	// stream. The combined string is handed to the session as its "memories"
	// block so it lands in the system prompt just like before.
	var contextPrelude string
	// Per-workspace context files (A7) come first so project conventions frame
	// everything below them.
	if r.contextLoader != nil {
		if ctxFiles := r.contextLoader.Load(req.Tenant, req.Agent); ctxFiles != "" {
			contextPrelude = ctxFiles
		}
	}
	if profileText := r.loadUserProfile(req.Tenant, req.User); profileText != "" {
		if contextPrelude != "" {
			contextPrelude += "\n\n" + profileText
		} else {
			contextPrelude = profileText
		}
	}
	if r.memoryStore != nil {
		if memories, err := r.memoryStore.Recall(req.Agent, req.Tenant); err == nil {
			// Tenant/user isolation: auto-extracted memories are scoped to the
			// user they were learned from (encoded in the Key as
			// "auto:<user>:..."). Drop any auto-memory belonging to a different
			// user so one user's facts never land in another user's prompt.
			// Non-auto memories (e.g. operator-curated "learned" facts) are not
			// user-scoped and remain visible agent-wide.
			memories = filterUserScopedMemories(memories, req.User)
			if memText := FormatForContext(memories); memText != "" {
				if contextPrelude != "" {
					contextPrelude += "\n\n" + memText
				} else {
					contextPrelude = memText
				}
			}
		}
	}
	// Switchable personality (F24): prepend the active persona's prompt. Returns
	// the prelude unchanged when no persona is selected, so it composes with the
	// context-file/profile/memory prelude above.
	if r.personalityStore != nil {
		r.personalityStore.SeedDefaults(req.Tenant)
		contextPrelude = r.personalityStore.Compose(req.Tenant, inst.session.ID(), contextPrelude)
	}
	if contextPrelude != "" {
		inst.session.SetMemories(contextPrelude)
	}

	// Load skill instructions on-demand
	if len(inst.config.Skills) > 0 {
		skillCtx, skillCancel := context.WithTimeout(context.Background(), 2*time.Second)
		skillResp, skillErr := r.busRequestTraced(skillCtx, ipc.Message{
			Source: "agent_runtime", Target: "skill_runtime", Topic: "skill.instructions",
			Payload: inst.config.Skills,
		})
		skillCancel()
		if skillErr == nil {
			if instructions, ok := skillResp.Payload.(map[string]string); ok {
				inst.session.SetSkillInstructions(formatSkillInstructions(instructions))
			}
		}
	}

	// Set last user for template expansion
	inst.session.SetLastUser(req.User)

	// Add user message to session
	inst.session.AddMessage(Message{Role: RoleUser, Content: req.Message})

	// Auto-compact history if it exceeds the summarize threshold. Before
	// dropping older context, nudge the agent to persist anything durable (A4).
	if inst.config.SummarizeThreshold > 0 && len(inst.session.History()) > inst.config.SummarizeThreshold {
		nudgeBeforeCompact(inst.session, inst.config.SummarizeThreshold/2)
	}

	// Get tool definitions for this agent.
	// If tools list contains "*", give access to ALL registered tools.
	var toolDefs []ToolDef
	if r.toolReg != nil && len(inst.config.Tools) > 0 {
		names := inst.config.Tools
		if len(names) == 1 && names[0] == "*" {
			names = r.toolReg.List()
		}
		// Per-session sandboxing (C15): an untrusted session's tool surface is
		// intersected with the sandbox-safe set — network/host-reaching tools
		// and host code-exec are stripped (the latter unless a docker backend
		// is declared), so the session can't reach beyond the allowlist.
		if inst.config.SandboxActive() {
			names = SafeToolset(names, inst.config.Sandbox.Backend)
		}
		toolDefs = r.toolReg.ToolDefs(names)
	}

	var toolsUsed []string

	// Aggregates spanning the whole agentic loop, snapshotted into the
	// agent.turn_completed event on the terminal turn.
	var toolCallCount, totalInputTokens, totalOutputTokens int

	// Agentic loop: call model, execute tools, repeat until no more tool calls
	for turn := 0; turn < inst.config.MaxTurns; turn++ {
		if inst.config.MaxTurns > 3 && turn == inst.config.MaxTurns-2 {
			inst.session.AddMessage(Message{
				Role:    RoleSystem,
				Content: "IMPORTANT: You have 2 tool-use turns remaining. Please wrap up your response and provide a final answer.",
			})
		}

		response, err := provider.Chat(context.Background(), inst.session.AssembleContext(), toolDefs)
		if err != nil {
			return ipc.Message{}, fmt.Errorf("model call failed: %w", err)
		}

		// Record usage with token counts from provider
		if r.usageStore != nil {
			go r.usageStore.Record(UsageRecord{
				Timestamp:    time.Now(),
				Tenant:       req.Tenant,
				Agent:        req.Agent,
				Provider:     inst.config.Model,
				InputTokens:  response.InputTokens,
				OutputTokens: response.OutputTokens,
				TotalTokens:  response.InputTokens + response.OutputTokens,
				DurationMs:   time.Since(chatStart).Milliseconds(),
			})
		}

		// Emit OTel token counters alongside the usage store write. We split
		// input/output so dashboards can chart them separately.
		observability.RecordLLMTokens(ctx, req.Tenant, inst.config.Model, "input", int64(response.InputTokens))
		observability.RecordLLMTokens(ctx, req.Tenant, inst.config.Model, "output", int64(response.OutputTokens))

		totalInputTokens += response.InputTokens
		totalOutputTokens += response.OutputTokens

		// Debit the tenant's token quota (fire-and-forget; survives missing module).
		if totalTok := int64(response.InputTokens + response.OutputTokens); totalTok > 0 {
			quotaRecord(r.bus, req.Tenant, totalTok)
		}

		inst.session.AddMessage(response)

		// If no tool calls, we're done
		if len(response.ToolCalls) == 0 {
			chatDuration := time.Since(chatStart)
			if chatDuration > 5*time.Second {
				logger.Warn("slow chat response", map[string]any{
					"agent": req.Agent, "tenant": req.Tenant,
					"duration_ms": chatDuration.Milliseconds(), "turns": turn + 1,
				})
			}
			r.publishActivity(req.Agent, req.Tenant, "chat_complete", "Response sent")

			// Record final metrics for this chat. We only emit the success
			// counter/duration once per request, on the terminal turn.
			observability.RecordChatRequest(ctx, req.Tenant, req.Agent, "ok")
			observability.RecordChatDuration(ctx, req.Tenant, req.Agent, float64(chatDuration.Milliseconds()))
			span.SetAttributes(attribute.Int("turns", turn+1))

			// Auto-memory extraction
			if inst.config.AutoMemory && r.memoryStore != nil && len(toolsUsed) > 0 {
				go r.extractMemories(req, inst, response.Content, toolsUsed)
			}

			// Record an activity summary for the user-model distiller. We
			// publish on a fire-and-forget Subscribe topic so the chat
			// returns immediately — durable write happens in the usermodel
			// module's goroutine. We deliberately keep the body short and
			// scrub it through the same secret/PII filters as the user-
			// visible response so leaked secrets don't end up in the
			// activity log.
			// Subagent isolation (A1 follow-up): a turn spawned by another
			// agent (orchestrate/delegate) must not write into the shared
			// user's durable memory — its internal Q&A is not the user's
			// activity. Skip the user-model activity record for those.
			if !isSubagent {
				activitySummary := MaskSecrets(req.Message)
				activitySummary = RedactPII(activitySummary)
				activitySummary = truncate(activitySummary, 200) + " -> " + truncate(MaskSecrets(RedactPII(response.Content)), 200)
				r.bus.Publish(ipc.Message{
					Source: "agent_runtime", Target: "usermodel", Topic: "usermodel.record_activity",
					Type:    ipc.MessageTypeEvent,
					Payload: map[string]string{"tenant": req.Tenant, "user": req.User, "summary": activitySummary},
				})
			}

			// Broadcast the completed-turn event for asynchronous consumers
			// (learning loop, recall indexer, trajectory capture). Fire-and-
			// forget: this returns before any subscriber runs, so it adds no
			// latency to the user's response.
			r.publishTurnCompleted(TurnRecord{
				Tenant:       req.Tenant,
				User:         req.User,
				Session:      inst.session.ID(),
				Agent:        req.Agent,
				Model:        inst.config.Model,
				UserMessage:  req.Message,
				Response:     response.Content,
				ToolsUsed:    toolsUsed,
				ToolCalls:    toolCallCount,
				Turns:        turn + 1,
				InputTokens:  totalInputTokens,
				OutputTokens: totalOutputTokens,
				TotalTokens:  totalInputTokens + totalOutputTokens,
				Outcome:      "ok",
				DurationMS:   chatDuration.Milliseconds(),
				StartedAt:    chatStart,
				Subagent:     isSubagent,
			})

			// Apply security filters: mask secrets and redact PII
			sanitized := MaskSecrets(response.Content)
			sanitized = RedactPII(sanitized)

			return ipc.Message{
				Type: ipc.MessageTypeResponse,
				Payload: ChatResponse{
					Agent:     req.Agent,
					Content:   sanitized,
					ToolsUsed: toolsUsed,
				},
			}, nil
		}

		// Execute tool calls
		toolCallCount += len(response.ToolCalls)
		for _, tc := range response.ToolCalls {
			// One span per tool call. We track duration on the span end as well
			// as in a histogram so OTel-naive backends (Prom) still get latency.
			// We deliberately don't pipe this span context into the tool's own
			// execution context (which uses WithToolCaller) because tools
			// re-anchor on their own caller metadata — not because we couldn't,
			// just to keep this patch surgical.
			toolSpanCtx, toolSpan := runtimeTracer.Start(ctx, "tool.call")
			toolSpan.SetAttributes(
				attribute.String("tool", tc.Name),
				attribute.String("agent", req.Agent),
			)
			toolSpanStart := time.Now()

			// finishToolSpan ends the span and records the metric set. status is
			// "ok" | "denied" | "error". Captured as a closure so each early
			// continue path in the loop body is one-liner clean.
			finishToolSpan := func(status string) {
				dur := time.Since(toolSpanStart)
				observability.RecordToolCall(toolSpanCtx, req.Tenant, req.Agent, tc.Name, status)
				observability.RecordToolDuration(toolSpanCtx, tc.Name, float64(dur.Milliseconds()))
				toolSpan.SetAttributes(attribute.String("status", status))
				if status == "error" || status == "denied" {
					toolSpan.SetStatus(codes.Error, status)
				}
				toolSpan.End()
			}

			// Check policy before execution
			decision := r.checkToolPolicy(req.Tenant, req.User, req.Agent, tc.Name)
			if decision == "deny" {
				inst.session.AddMessage(Message{
					Role:        RoleTool,
					ToolResults: []ToolResult{{CallID: tc.ID, Content: "DENIED: Policy does not allow " + tc.Name, IsError: true}},
				})
				toolsUsed = append(toolsUsed, tc.Name+"(denied)")
				finishToolSpan("denied")
				continue
			}
			if decision == "require_approval" {
				// Submit approval request
				approvalCtx, approvalCancel := context.WithTimeout(context.Background(), 5*time.Second)
				approvalResp, submitErr := r.busRequestTraced(approvalCtx, ipc.Message{
					Source: "agent_runtime", Target: "policy", Topic: "policy.approval.submit",
					Payload: map[string]string{"tenant": req.Tenant, "agent": req.Agent, "user": req.User, "tool": tc.Name, "action": "tool_call"},
				})
				approvalCancel()

				approvalID := ""
				if submitErr == nil {
					if id, ok := approvalResp.Payload.(string); ok {
						approvalID = id
					}
				}

				r.publishActivity(req.Agent, req.Tenant, "approval_needed", "Tool "+tc.Name+" requires approval (ID: "+approvalID+")")

				status := r.waitForApproval(approvalID)
				if status != "approved" {
					inst.session.AddMessage(Message{
						Role:        RoleTool,
						ToolResults: []ToolResult{{CallID: tc.ID, Content: fmt.Sprintf("APPROVAL %s: %s", strings.ToUpper(status), tc.Name), IsError: true}},
					})
					toolsUsed = append(toolsUsed, tc.Name+"("+status+")")
					finishToolSpan("denied")
					continue
				}
				// Approved — fall through to execute the tool below
			}

			r.publishActivity(req.Agent, req.Tenant, "tool_exec", "Executing: "+tc.Name)

			// Publish progress event so the originating channel can show activity
			if req.Channel != "" && req.ChannelID != "" {
				r.bus.Publish(ipc.Message{
					Source: "agent_runtime",
					Topic:  "agent.progress",
					Payload: ProgressEvent{
						Agent:     req.Agent,
						Tenant:    req.Tenant,
						Channel:   req.Channel,
						ChannelID: req.ChannelID,
						ToolName:  tc.Name,
						Status:    "running",
						Message:   fmt.Sprintf("_Running `%s`..._", tc.Name),
					},
				})
			}

			// Execute the tool
			toolsUsed = append(toolsUsed, tc.Name)

			var result string
			var isError bool

			if r.toolReg == nil {
				result = "tool registry not available"
				isError = true
			} else {
				// Inject agent secrets into tool input so tools can use per-agent credentials
				toolInput := tc.Input
				if len(inst.config.Secrets) > 0 {
					merged := make(map[string]string)
					for k, v := range tc.Input {
						merged[k] = v
					}
					for k, v := range inst.config.Secrets {
						lk := strings.ToLower(k)
						if _, exists := merged[lk]; !exists {
							merged[lk] = v
						}
					}
					toolInput = merged
				}
				var execErr error
				toolStart := time.Now()
				toolCtx := WithToolCaller(context.Background(), req.Tenant, req.Agent, req.User)
				// Carry the orchestration batch id (set by orchestrate as the
				// fan-out TraceID) so a worker's context_read scopes to its own
				// batch's shared channel (#48). Gate on isSubagent: the bus
				// auto-assigns a TraceID to every message, and top-level chats
				// set it from X-Request-ID, so binding on a non-empty TraceID
				// alone would give every solo turn a channel and let a caller
				// read a batch's notes by replaying its id. Only orchestrate/
				// delegate children get a channel.
				if isSubagent && msg.TraceID != "" {
					toolCtx = WithChannel(toolCtx, msg.TraceID)
				}
				result, execErr = r.executeToolWithRetry(toolCtx, tc.Name, toolInput)
				toolDuration := time.Since(toolStart)
				if toolDuration > 2*time.Second {
					logger.Warn("slow tool execution", map[string]any{
						"tool": tc.Name, "duration_ms": toolDuration.Milliseconds(),
					})
				}
				if execErr != nil {
					// A tool that echoes its arguments in the error message can
					// surface the per-agent secrets we injected above back to the
					// model. Redact every injected secret value from the error
					// text before it lands in the session/tool-result.
					result = redactSecretValues(execErr.Error(), inst.config.Secrets)
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

			r.publishActivity(req.Agent, req.Tenant, "tool_result", tc.Name+" completed")

			// Close out the tool span with terminal status based on isError.
			if isError {
				finishToolSpan("error")
			} else {
				finishToolSpan("ok")
			}
		}
	}

	// Loop fell through max turns without producing a final assistant message.
	span.SetStatus(codes.Error, "max turns exceeded")
	observability.RecordChatRequest(ctx, req.Tenant, req.Agent, "error")
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

func (r *Runtime) handleGet(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	tenant, name := params["tenant"], params["name"]
	key := tenant + "/" + name

	r.mu.RLock()
	inst, exists := r.agents[key]
	r.mu.RUnlock()

	if exists {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: inst.config}, nil
	}

	// Try loading from store
	if r.store != nil {
		agents, err := r.store.LoadAgents()
		if err == nil {
			for _, cfg := range agents {
				if cfg.Tenant == tenant && cfg.Name == name {
					return ipc.Message{Type: ipc.MessageTypeResponse, Payload: cfg}, nil
				}
			}
		}
	}

	return ipc.Message{}, fmt.Errorf("agent %q not found in tenant %q", name, tenant)
}

func (r *Runtime) handleDelete(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	tenant, name := params["tenant"], params["name"]
	key := tenant + "/" + name

	r.mu.Lock()
	delete(r.agents, key)
	r.mu.Unlock()

	if r.store != nil {
		r.store.DeleteAgent(tenant, name)
		r.store.DeleteSession("sess_" + tenant + "_" + name)
	}

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "deleted"}, nil
}

func (r *Runtime) handleSessions(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	tenant, name := params["tenant"], params["name"]
	prefix := "sess_" + tenant + "_" + name

	if r.store == nil {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: []string{}}, nil
	}

	all, err := r.store.ListSessions()
	if err != nil {
		return ipc.Message{}, err
	}

	var filtered []string
	for _, id := range all {
		if len(id) >= len(prefix) && id[:len(prefix)] == prefix {
			filtered = append(filtered, id)
		}
	}
	if filtered == nil {
		filtered = []string{}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: filtered}, nil
}

func (r *Runtime) handleSessionMessages(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	tenant, sessionID := params["tenant"], params["sid"]
	if tenant == "" || sessionID == "" {
		return ipc.Message{}, fmt.Errorf("agent.session.messages: tenant and sid are required")
	}
	if r.store == nil {
		return ipc.Message{}, fmt.Errorf("session store not configured")
	}
	// Tenant isolation: a session id is "sess_<tenant>_<name>". Reject any id
	// that does not belong to the caller's tenant so a raw id cannot read
	// another tenant's history.
	if !strings.HasPrefix(sessionID, "sess_"+tenant+"_") {
		return ipc.Message{}, fmt.Errorf("session %q not found in tenant %q", sessionID, tenant)
	}

	_, messages, err := r.store.LoadSession(sessionID)
	if err != nil {
		return ipc.Message{}, err
	}

	// Convert to serializable format
	type msgOut struct {
		Role        string       `json:"role"`
		Content     string       `json:"content"`
		ToolCalls   []ToolCall   `json:"tool_calls,omitempty"`
		ToolResults []ToolResult `json:"tool_results,omitempty"`
	}

	out := make([]msgOut, len(messages))
	for i, m := range messages {
		// Apply secret masking and PII redaction to all message content
		content := MaskSecrets(m.Content)
		content = RedactPII(content)
		out[i] = msgOut{
			Role:        m.Role.String(),
			Content:     content,
			ToolCalls:   m.ToolCalls,
			ToolResults: m.ToolResults,
		}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: out}, nil
}

func (r *Runtime) handleMemories(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	tenant, name := params["tenant"], params["name"]

	if r.memoryStore == nil {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: []Memory{}}, nil
	}

	memories, err := r.memoryStore.Recall(name, tenant)
	if err != nil {
		return ipc.Message{}, err
	}
	// Mask secrets and PII in memory content before returning
	for i := range memories {
		memories[i].Content = MaskSecrets(memories[i].Content)
		memories[i].Content = RedactPII(memories[i].Content)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: memories}, nil
}

func (r *Runtime) handleSessionClear(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	tenant, name := params["tenant"], params["name"]
	key := tenant + "/" + name

	r.mu.RLock()
	inst, exists := r.agents[key]
	r.mu.RUnlock()

	if !exists {
		return ipc.Message{}, fmt.Errorf("agent %q not found in tenant %q", name, tenant)
	}

	inst.session.ClearHistory()
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "cleared"}, nil
}

func (r *Runtime) handleUpdate(msg ipc.Message) (ipc.Message, error) {
	cfg, ok := msg.Payload.(AgentConfig)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected AgentConfig, got %T", msg.Payload)
	}
	key := cfg.Tenant + "/" + cfg.Name

	r.mu.Lock()
	inst, exists := r.agents[key]
	if !exists {
		r.mu.Unlock()
		return ipc.Message{}, fmt.Errorf("agent %q not found in tenant %q", cfg.Name, cfg.Tenant)
	}
	if cfg.Model != "" {
		inst.config.Model = cfg.Model
	}
	if cfg.SystemPrompt != "" {
		inst.config.SystemPrompt = cfg.SystemPrompt
	}
	if len(cfg.Tools) > 0 {
		inst.config.Tools = cfg.Tools
	}
	if cfg.MaxTurns > 0 {
		inst.config.MaxTurns = cfg.MaxTurns
	}
	if cfg.MaxHistory > 0 {
		inst.config.MaxHistory = cfg.MaxHistory
	}
	if cfg.SummarizeThreshold > 0 {
		inst.config.SummarizeThreshold = cfg.SummarizeThreshold
	}
	if cfg.RateLimit > 0 {
		inst.config.RateLimit = cfg.RateLimit
	}
	if len(cfg.Skills) > 0 {
		inst.config.Skills = cfg.Skills
	}
	if len(cfg.MCPServers) > 0 {
		inst.config.MCPServers = cfg.MCPServers
	}
	if len(cfg.Secrets) > 0 {
		inst.config.Secrets = cfg.Secrets
	}
	// AutoMemory is a bool — update if the config explicitly sets it
	// (we always accept this field since false is a valid value)
	inst.config.AutoMemory = cfg.AutoMemory
	r.mu.Unlock()

	if r.store != nil {
		r.store.SaveAgent(inst.config)
		r.store.SaveAgentVersion(inst.config)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "updated"}, nil
}

func (r *Runtime) publishActivity(agent, tenant, eventType, detail string) {
	r.bus.Publish(ipc.Message{
		Source: "agent_runtime",
		Topic:  "agent.activity",
		Payload: ActivityEvent{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Agent:     agent,
			Tenant:    tenant,
			Type:      eventType,
			Detail:    detail,
		},
	})
}

// publishTurnCompleted broadcasts a TurnRecord on TopicTurnCompleted for
// asynchronous consumers (learning loop, recall indexer, trajectory capture).
// Delivery is fire-and-forget and fans out to every subscriber of the topic,
// so this never blocks or adds latency to the user's response. The record
// carries raw text; consumers must sanitize before persisting or displaying.
func (r *Runtime) publishTurnCompleted(rec TurnRecord) {
	if r.bus == nil {
		return
	}
	r.bus.Publish(ipc.Message{
		Source:  "agent_runtime",
		Topic:   TopicTurnCompleted,
		Type:    ipc.MessageTypeEvent,
		Payload: rec,
	})
}

// waitForApproval polls the policy engine for approval status until approved,
// denied, expired, or a 5-minute timeout is reached.
func (r *Runtime) waitForApproval(approvalID string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return "timeout"
		case <-time.After(2 * time.Second):
			statusCtx, statusCancel := context.WithTimeout(context.Background(), 2*time.Second)
			resp, err := r.busRequestTraced(statusCtx, ipc.Message{
				Source: "agent_runtime", Target: "policy", Topic: "approval.get",
				Payload: approvalID,
			})
			statusCancel()
			if err == nil {
				if status, ok := resp.Payload.(string); ok {
					if status == "approved" || status == "denied" || status == "expired" {
						return status
					}
				}
			}
		}
	}
}

// executeToolWithRetry wraps tool execution with exponential backoff retry logic.
// It retries up to maxRetries times on failure, with exponentially increasing delays.
func (r *Runtime) executeToolWithRetry(ctx context.Context, toolName string, input map[string]string) (string, error) {
	maxRetries := 3
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		result, err := r.toolReg.Execute(ctx, toolName, input)
		if err == nil {
			return result, nil
		}
		lastErr = err
		// Don't retry Loomfeed-specific limits — but DO retry other 429s (like Firecrawl)
		errStr := err.Error()
		isLoomfeedLimit := (strings.Contains(errStr, "limited to") ||
			strings.Contains(errStr, "duplicate") ||
			strings.Contains(errStr, "maximum number") ||
			strings.Contains(errStr, "already posted") ||
			strings.Contains(errStr, "prohibited content") ||
			(strings.Contains(errStr, "429") && strings.Contains(errStr, "Alatirok")))
		if isLoomfeedLimit {
			logger.Warn("tool execution blocked by server limit, not retrying", map[string]any{
				"tool": toolName, "error": errStr,
			})
			return "", lastErr
		}
		if attempt < maxRetries {
			backoff := time.Duration(1<<uint(attempt)) * 100 * time.Millisecond
			logger.Warn("tool execution failed, retrying", map[string]any{
				"tool": toolName, "attempt": attempt + 1, "backoff_ms": backoff.Milliseconds(), "error": err.Error(),
			})
			time.Sleep(backoff)
		}
	}
	return "", lastErr
}

func formatSkillInstructions(instructions map[string]string) string {
	if len(instructions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Active Skills\n\n")
	for name, instr := range instructions {
		sb.WriteString("### Skill: ")
		sb.WriteString(name)
		sb.WriteString("\n\n")
		sb.WriteString(instr)
		sb.WriteString("\n\n---\n\n")
	}
	return sb.String()
}

// Shared-context coordination topics (#48). Write is reached only via the
// orchestrate tool (the coordinator path); read is reached via the read-only
// context_read tool that worker subagents carry.
const (
	TopicContextWrite = "agent.context.write"
	TopicContextRead  = "agent.context.read"
)

// ContextReadRequest asks for every shared note in (Tenant, Channel).
type ContextReadRequest struct {
	Tenant  string `json:"tenant"`
	Channel string `json:"channel"`
}

// ContextReadResult carries the notes back to a worker's context_read tool.
type ContextReadResult struct {
	Entries []SharedContextEntry `json:"entries"`
}

// handleContextWrite persists a coordinator's shared note. The runtime forces
// the tenant/channel/author from the trusted call rather than trusting the
// model, and runs content through the same redaction as every other persisted
// surface.
func (r *Runtime) handleContextWrite(msg ipc.Message) (ipc.Message, error) {
	// Only the coordinator path (the orchestrate tool) may write the shared
	// channel. Workers carry no write tool, but the bus topic is otherwise
	// reachable by any in-process sender, so authorize by source rather than
	// relying on tool-surface omission alone (#48).
	if msg.Source != "orchestrate" {
		return ipc.Message{}, fmt.Errorf("agent.context.write: not authorized for source %q", msg.Source)
	}
	e, ok := msg.Payload.(SharedContextEntry)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected SharedContextEntry, got %T", msg.Payload)
	}
	if r.contextStore == nil {
		return ipc.Message{}, fmt.Errorf("shared context store not configured")
	}
	if e.Tenant == "" || e.Channel == "" || e.Key == "" {
		return ipc.Message{}, fmt.Errorf("agent.context.write: tenant, channel and key are required")
	}
	// Both the value AND the model-chosen key are persisted and later rendered
	// into a worker's prompt, so redact both.
	e.Key = RedactPII(MaskSecrets(e.Key))
	e.Content = RedactPII(MaskSecrets(e.Content))
	if err := r.contextStore.Write(e); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

// handleContextRead returns the notes for a (tenant, channel). Tenant and
// channel are both required and never crossed, so a worker can only read its
// own batch's channel within its own tenant.
func (r *Runtime) handleContextRead(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(ContextReadRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected ContextReadRequest, got %T", msg.Payload)
	}
	if r.contextStore == nil {
		return ipc.Message{}, fmt.Errorf("shared context store not configured")
	}
	entries, err := r.contextStore.Read(req.Tenant, req.Channel)
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: ContextReadResult{Entries: entries}}, nil
}

// handleMemorySave persists a long-term memory. Used by the learning loop to
// record what it learned from a completed turn. Content is run through the
// same secret/PII filters as chat output before it lands on disk.
func (r *Runtime) handleMemorySave(msg ipc.Message) (ipc.Message, error) {
	m, ok := msg.Payload.(Memory)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected Memory, got %T", msg.Payload)
	}
	if r.memoryStore == nil {
		return ipc.Message{}, fmt.Errorf("memory store not configured")
	}
	if m.Tenant == "" || m.Agent == "" || m.Content == "" {
		return ipc.Message{}, fmt.Errorf("agent.memory.save: tenant, agent and content are required")
	}
	m.Content = RedactPII(MaskSecrets(m.Content))
	if m.Key == "" {
		m.Key = "learned"
	}
	if err := r.memoryStore.Save(m); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (r *Runtime) handleMemoryDelete(msg ipc.Message) (ipc.Message, error) {
	// Authz (fail-closed): a memory may only be deleted by its owning
	// (tenant, agent). The caller MUST supply the tenant and agent from the
	// authenticated request path; a bare id with no owner is rejected so one
	// tenant cannot delete another tenant's memory by guessing/replaying ids.
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string{id,tenant,agent}, got %T", msg.Payload)
	}
	memID, tenant, agentName := params["id"], params["tenant"], params["agent"]
	if memID == "" || tenant == "" || agentName == "" {
		return ipc.Message{}, fmt.Errorf("agent.memory.delete: id, tenant and agent are required")
	}
	if r.memoryStore == nil {
		return ipc.Message{}, fmt.Errorf("memory store not configured")
	}
	// Verify ownership before deleting: the id must belong to a memory owned
	// by this (agent, tenant). We scope the lookup to the caller's tenant/agent
	// so a cross-tenant id never matches.
	owned, err := r.memoryStore.Recall(agentName, tenant)
	if err != nil {
		return ipc.Message{}, err
	}
	isOwned := false
	for _, m := range owned {
		if m.ID == memID {
			isOwned = true
			break
		}
	}
	if !isOwned {
		// Don't disclose whether the id exists under another owner.
		return ipc.Message{}, fmt.Errorf("agent.memory.delete: memory not found")
	}
	if err := r.memoryStore.Delete(memID); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "deleted"}, nil
}

func (r *Runtime) handleSearch(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string{tenant,query}, got %T", msg.Payload)
	}
	tenant, query := params["tenant"], params["query"]
	if tenant == "" {
		return ipc.Message{}, fmt.Errorf("agent.search: tenant is required")
	}
	if r.store == nil {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: []SearchResult{}}, nil
	}
	results, err := r.store.SearchMessages(query, tenant)
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: results}, nil
}

func (r *Runtime) handleVersions(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	if r.store == nil {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: []any{}}, nil
	}
	versions, err := r.store.ListAgentVersions(params["tenant"], params["name"])
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: versions}, nil
}

func (r *Runtime) handleRollback(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	if r.store == nil {
		return ipc.Message{}, fmt.Errorf("store not configured")
	}
	version := 0
	fmt.Sscanf(params["version"], "%d", &version)
	cfg, err := r.store.GetAgentVersion(params["tenant"], params["name"], version)
	if err != nil {
		return ipc.Message{}, err
	}
	// Apply the old config
	key := cfg.Tenant + "/" + cfg.Name
	r.mu.Lock()
	if inst, exists := r.agents[key]; exists {
		inst.config = *cfg
	}
	r.mu.Unlock()
	r.store.SaveAgent(*cfg)
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "rolled back to version " + params["version"]}, nil
}

func (r *Runtime) extractMemories(req ChatRequest, inst *agentInstance, lastResponse string, toolsUsed []string) {
	if r.memoryStore == nil || lastResponse == "" {
		return
	}

	history := inst.session.History()
	if len(history) < 2 {
		return
	}

	// Find the user's last question
	var userQuery string
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == RoleUser {
			userQuery = history[i].Content
			break
		}
	}
	if userQuery == "" {
		return
	}

	// Use the LLM to extract meaningful memories from the conversation
	r.mu.RLock()
	provider, hasProvider := r.providers[inst.config.Model]
	r.mu.RUnlock()

	var summary string
	if hasProvider {
		// Ask the LLM to extract key facts
		extractCtx, extractCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer extractCancel()

		extractPrompt := fmt.Sprintf(
			"Extract 1-3 key facts worth remembering from this exchange. "+
				"Return only the facts, one per line. No preamble.\n\n"+
				"User: %s\nAssistant: %s",
			truncate(userQuery, 300), truncate(lastResponse, 500))

		extractResp, err := provider.Chat(extractCtx, []Message{
			{Role: RoleUser, Content: extractPrompt},
		}, nil)
		if err == nil && extractResp.Content != "" {
			summary = extractResp.Content
		}
	}

	// Fallback to heuristic if LLM extraction failed
	if summary == "" {
		summary = fmt.Sprintf("User asked: %s | Tools used: %s | Response: %s",
			truncate(userQuery, 100), strings.Join(toolsUsed, ", "), truncate(lastResponse, 200))
	}

	// Namespace the key by user so recall can scope auto-memories to the user
	// they were learned from (the Memory schema has no user column). Sanitize
	// the user id so its delimiter can't be spoofed via the user string.
	memory := Memory{
		Agent:   req.Agent,
		Tenant:  req.Tenant,
		Key:     autoMemoryKey(req.User, userQuery),
		Content: summary,
	}

	if err := r.memoryStore.Save(memory); err != nil {
		logger.Warn("auto-memory save failed", map[string]any{"error": err.Error()})
	} else {
		logger.Info("auto-memory saved", map[string]any{"agent": req.Agent, "key": memory.Key})
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// redactSecretValues replaces every per-agent secret value found in text with
// the standard ***REDACTED*** marker. Used to scrub tool-error surfaces so a
// tool that echoes its (secret-injected) arguments can't leak credentials back
// to the model. Short/empty values are skipped to avoid masking unrelated text.
func redactSecretValues(text string, secrets map[string]string) string {
	if text == "" || len(secrets) == 0 {
		return text
	}
	for _, v := range secrets {
		if len(v) < 4 {
			continue
		}
		text = strings.ReplaceAll(text, v, "***REDACTED***")
	}
	return text
}

// autoMemoryPrefix is the namespace under which per-user auto-extracted
// memories are stored. The user id is encoded into the key (the Memory schema
// has no user column) so recall can isolate them per user.
const autoMemoryPrefix = "auto:"

// sanitizeMemoryUser strips the ':' key delimiter from a user id so a crafted
// user string can't break out of its own namespace.
func sanitizeMemoryUser(user string) string {
	return strings.ReplaceAll(user, ":", "_")
}

// autoMemoryKey builds the namespaced key "auto:<user>:<query-snippet>".
func autoMemoryKey(user, userQuery string) string {
	return autoMemoryPrefix + sanitizeMemoryUser(user) + ":" + truncate(userQuery, 50)
}

// filterUserScopedMemories drops auto-extracted memories that belong to a
// different user. A memory is user-scoped when its key has the "auto:<user>:"
// prefix; only those matching the supplied user are kept. Any other memory
// (operator-curated, legacy "auto:" without a user segment, etc.) is left
// untouched so non-user-scoped knowledge stays agent-wide.
func filterUserScopedMemories(memories []Memory, user string) []Memory {
	want := autoMemoryPrefix + sanitizeMemoryUser(user) + ":"
	out := memories[:0:0]
	for _, m := range memories {
		if strings.HasPrefix(m.Key, autoMemoryPrefix) {
			// Legacy auto-memories saved before per-user scoping have no user
			// segment ("auto:<query>"); treat them as another user's data and
			// drop them to fail closed on isolation.
			if !strings.HasPrefix(m.Key, want) {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}
