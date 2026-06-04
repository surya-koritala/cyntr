package recall

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/jobs"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

// Module indexes agent turns for full-text recall and serves recall.search.
//
// It subscribes to agent.turn_completed (F0.1), writes the user message and
// assistant response into the recall index, and — when a queue and summarizer
// are configured — enqueues a debounced summarize job (F0.2) once enough new
// messages have accrued for the session.
type Module struct {
	bus        *ipc.Bus
	store      *Store
	queue      *jobs.Queue
	summarizer *Summarizer

	summarizeEvery int
	logf           func(string, map[string]any)
}

// Option configures the Module.
type Option func(*Module)

// WithQueue wires the background job queue used for summarization.
func WithQueue(q *jobs.Queue) Option { return func(m *Module) { m.queue = q } }

// WithSummarizer attaches the session summarizer.
func WithSummarizer(s *Summarizer) Option { return func(m *Module) { m.summarizer = s } }

// WithSummarizeEvery sets how many new messages since the last summary trigger
// a re-summarize (default 6). This debounces summary jobs per session.
func WithSummarizeEvery(n int) Option {
	return func(m *Module) {
		if n > 0 {
			m.summarizeEvery = n
		}
	}
}

// WithLogger attaches a structured logger.
func WithLogger(fn func(string, map[string]any)) Option { return func(m *Module) { m.logf = fn } }

// New constructs a recall Module backed by store.
func New(store *Store, opts ...Option) *Module {
	m := &Module{store: store, summarizeEvery: 6}
	for _, o := range opts {
		o(m)
	}
	return m
}

// Store returns the underlying store (so main.go can share it).
func (m *Module) Store() *Store { return m.store }

func (m *Module) Name() string           { return "recall" }
func (m *Module) Dependencies() []string { return nil }

func (m *Module) Init(ctx context.Context, svc *kernel.Services) error {
	m.bus = svc.Bus
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	m.bus.Handle("recall", TopicSearch, m.handleSearch)
	// Indexing is fire-and-forget so the chat path never waits on us.
	m.bus.Subscribe("recall", agent.TopicTurnCompleted, m.handleTurnCompleted)
	if m.queue != nil && m.summarizer != nil {
		m.queue.Register(JobKindSummarize, m.handleSummarizeJob)
	}
	return nil
}

func (m *Module) Stop(ctx context.Context) error { return nil }

func (m *Module) Health(ctx context.Context) kernel.HealthStatus {
	if m.store == nil {
		return kernel.HealthStatus{Healthy: false, Message: "store not configured"}
	}
	return kernel.HealthStatus{Healthy: true, Message: "ok"}
}

// handleSearch serves the recall.search request/response topic.
func (m *Module) handleSearch(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(SearchRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("recall.search: expected SearchRequest, got %T", msg.Payload)
	}
	snippets, err := m.store.Search(req.Tenant, req.User, req.Query, req.Limit)
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: SearchResult{Snippets: snippets}}, nil
}

// handleTurnCompleted indexes the just-finished turn and, if warranted,
// enqueues a summarize job. Errors are logged, not returned — this is an
// event subscriber and must never disrupt the publisher.
func (m *Module) handleTurnCompleted(msg ipc.Message) (ipc.Message, error) {
	rec, ok := msg.Payload.(agent.TurnRecord)
	if !ok {
		return ipc.Message{}, nil
	}
	if rec.Tenant == "" || rec.User == "" || rec.Session == "" {
		return ipc.Message{}, nil
	}
	now := time.Now().UTC()

	// Sanitize before persisting: TurnRecord carries raw text by contract.
	if user := sanitize(rec.UserMessage); user != "" {
		if err := m.store.Index(IndexedMessage{
			Tenant: rec.Tenant, User: rec.User, Session: rec.Session,
			Role: "user", Text: user, CreatedAt: rec.StartedAt, MsgID: rec.Session + ":u",
		}); err != nil {
			m.log("recall: index user message failed", map[string]any{"error": err.Error()})
		}
	}
	if resp := sanitize(rec.Response); resp != "" {
		if err := m.store.Index(IndexedMessage{
			Tenant: rec.Tenant, User: rec.User, Session: rec.Session,
			Role: "assistant", Text: resp, CreatedAt: now, MsgID: rec.Session + ":a",
		}); err != nil {
			m.log("recall: index response failed", map[string]any{"error": err.Error()})
		}
	}

	m.maybeEnqueueSummary(rec.Tenant, rec.User, rec.Session)
	return ipc.Message{}, nil
}

// maybeEnqueueSummary enqueues a summarize job only once enough new messages
// have accrued since the last summary — a per-session debounce that keeps the
// queue from filling with redundant summarize work.
func (m *Module) maybeEnqueueSummary(tenant, user, session string) {
	if m.queue == nil || m.summarizer == nil {
		return
	}
	total, err := m.store.MessageCount(tenant, user, session)
	if err != nil {
		return
	}
	_, summarized, err := m.store.Summary(tenant, user, session)
	if err != nil {
		return
	}
	if total-summarized < m.summarizeEvery {
		return
	}
	payload, _ := json.Marshal(summarizeJob{Tenant: tenant, User: user, Session: session})
	if _, err := m.queue.Enqueue(tenant, JobKindSummarize, payload, time.Time{}); err != nil {
		m.log("recall: enqueue summarize failed", map[string]any{"error": err.Error()})
	}
}

type summarizeJob struct {
	Tenant  string `json:"tenant"`
	User    string `json:"user"`
	Session string `json:"session"`
}

// handleSummarizeJob is the kernel/jobs handler for recall.summarize.
func (m *Module) handleSummarizeJob(ctx context.Context, job jobs.Job) error {
	var p summarizeJob
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return fmt.Errorf("recall: bad summarize payload: %w", err)
	}
	return m.summarizer.SummarizeSession(ctx, p.Tenant, p.User, p.Session)
}

func (m *Module) log(msg string, fields map[string]any) {
	if m.logf != nil {
		m.logf(msg, fields)
	}
}

// sanitize runs the same secret/PII filters the chat response path uses before
// any recalled text is persisted to disk.
func sanitize(s string) string {
	return agent.RedactPII(agent.MaskSecrets(s))
}
