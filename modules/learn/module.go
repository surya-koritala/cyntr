package learn

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/jobs"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/skill"
)

// Module is the closed-learning-loop kernel module.
type Module struct {
	bus     *ipc.Bus
	queue   *jobs.Queue
	reflect ReflectFunc

	enabled      bool
	minToolCalls int
	logf         func(string, map[string]any)
}

// Option configures the Module.
type Option func(*Module)

// WithQueue wires the background job queue that runs reflections.
func WithQueue(q *jobs.Queue) Option { return func(m *Module) { m.queue = q } }

// WithReflectFunc sets the LLM after-action review function.
func WithReflectFunc(fn ReflectFunc) Option { return func(m *Module) { m.reflect = fn } }

// WithMinToolCalls sets the complexity threshold (default 3).
func WithMinToolCalls(n int) Option {
	return func(m *Module) {
		if n > 0 {
			m.minToolCalls = n
		}
	}
}

// WithLogger attaches a structured logger.
func WithLogger(fn func(string, map[string]any)) Option { return func(m *Module) { m.logf = fn } }

// New constructs a learn Module. enabled gates the whole loop: when false (the
// default in main.go unless CYNTR_LEARN_ENABLED=true), it registers nothing
// and the agent runs exactly as before.
func New(enabled bool, opts ...Option) *Module {
	m := &Module{enabled: enabled, minToolCalls: DefaultMinToolCalls}
	for _, o := range opts {
		o(m)
	}
	return m
}

func (m *Module) Name() string           { return "learn" }
func (m *Module) Dependencies() []string { return nil }

func (m *Module) Init(ctx context.Context, svc *kernel.Services) error {
	m.bus = svc.Bus
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	if !m.active() {
		m.log("learning loop disabled", map[string]any{
			"enabled": m.enabled, "have_queue": m.queue != nil, "have_reflect": m.reflect != nil,
		})
		return nil
	}
	m.queue.Register(JobKindReflect, m.handleReflectJob)
	m.bus.Subscribe("learn", agent.TopicTurnCompleted, m.handleTurnCompleted)
	m.log("learning loop enabled", map[string]any{"min_tool_calls": m.minToolCalls})
	return nil
}

func (m *Module) Stop(ctx context.Context) error { return nil }

func (m *Module) Health(ctx context.Context) kernel.HealthStatus {
	return kernel.HealthStatus{Healthy: true, Message: fmt.Sprintf("active=%v", m.active())}
}

func (m *Module) active() bool { return m.enabled && m.queue != nil && m.reflect != nil }

// handleTurnCompleted enqueues a reflection job when a turn is complex enough.
// Event subscriber: errors are logged, never returned.
func (m *Module) handleTurnCompleted(msg ipc.Message) (ipc.Message, error) {
	rec, ok := msg.Payload.(agent.TurnRecord)
	if !ok || !shouldReflect(rec, m.minToolCalls) {
		return ipc.Message{}, nil
	}
	payload, err := json.Marshal(rec)
	if err != nil {
		return ipc.Message{}, nil
	}
	if _, err := m.queue.Enqueue(rec.Tenant, JobKindReflect, payload, time.Time{}); err != nil {
		m.log("learn: enqueue reflection failed", map[string]any{"error": err.Error()})
	}
	return ipc.Message{}, nil
}

// handleReflectJob runs the after-action review and persists what it learned:
// a memory and, when warranted, a proposed skill (which itself is approval-
// gated by the skill module — never auto-installed with risky capabilities).
func (m *Module) handleReflectJob(ctx context.Context, job jobs.Job) error {
	var rec agent.TurnRecord
	if err := json.Unmarshal(job.Payload, &rec); err != nil {
		return fmt.Errorf("learn: bad reflect payload: %w", err)
	}

	content, err := m.reflect(ctx, fmt.Sprintf(reflectPrompt, buildTranscript(rec)))
	if err != nil {
		return fmt.Errorf("learn: reflect: %w", err)
	}
	r, err := parseReflection(content)
	if err != nil {
		return fmt.Errorf("learn: parse reflection: %w", err)
	}

	if mem := strings.TrimSpace(r.Memory); mem != "" {
		if _, err := m.bus.Request(ctx, ipc.Message{
			Source: "learn", Target: "agent_runtime", Topic: "agent.memory.save",
			Payload: agent.Memory{Tenant: rec.Tenant, Agent: rec.Agent, Key: "learned", Content: mem},
		}); err != nil {
			m.log("learn: memory save failed", map[string]any{"error": err.Error()})
		}
	}

	if r.Skill != nil && strings.TrimSpace(r.Skill.Name) != "" && strings.TrimSpace(r.Skill.Instructions) != "" {
		if _, err := m.bus.Request(ctx, ipc.Message{
			Source: "learn", Target: "skill_runtime", Topic: skill.TopicPropose,
			Payload: skill.ProposeRequest{
				Tenant:       rec.Tenant,
				Name:         r.Skill.Name,
				Description:  r.Skill.Description,
				Instructions: r.Skill.Instructions,
				SourceAgent:  rec.Agent,
			},
		}); err != nil {
			m.log("learn: skill proposal failed", map[string]any{"error": err.Error()})
		}
	}
	return nil
}

func (m *Module) log(msg string, fields map[string]any) {
	if m.logf != nil {
		m.logf(msg, fields)
	}
}
