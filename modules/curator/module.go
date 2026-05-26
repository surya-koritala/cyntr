package curator

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// IPC topics owned by the curator module.
const (
	TopicRecord         = "curator.record"
	TopicScores         = "curator.scores"
	TopicSuggestPrune   = "curator.suggest_prune"
	ModuleName          = "curator"
)

// Module is the kernel-facing wrapper around the curator store +
// score logic. It exposes three IPC topics:
//
//	curator.record         — fire-and-forget event with Invocation payload (Subscribe)
//	curator.scores         — request/response, optional ScoresFilter payload
//	curator.suggest_prune  — request/response, returns []PruneSuggestion
type Module struct {
	dbPath string
	bus    *ipc.Bus
	store  *Store
	sub    *ipc.Subscription
	now    func() time.Time // injectable for tests
}

// New constructs a Curator module that will persist to dbPath when
// Init is called. Pass an in-memory path for tests.
func New(dbPath string) *Module {
	return &Module{
		dbPath: dbPath,
		now:    func() time.Time { return time.Now().UTC() },
	}
}

func (m *Module) Name() string           { return ModuleName }
func (m *Module) Dependencies() []string { return nil }

func (m *Module) Init(ctx context.Context, svc *kernel.Services) error {
	m.bus = svc.Bus
	store, err := NewStore(m.dbPath)
	if err != nil {
		return fmt.Errorf("curator init: %w", err)
	}
	m.store = store
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	// curator.record is published fire-and-forget. We attach via
	// Subscribe so the agent loop never blocks on us.
	m.sub = m.bus.Subscribe(ModuleName, TopicRecord, m.handleRecord)
	m.bus.Handle(ModuleName, TopicScores, m.handleScores)
	m.bus.Handle(ModuleName, TopicSuggestPrune, m.handleSuggestPrune)
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	if m.sub != nil {
		m.sub.Cancel()
	}
	if m.store != nil {
		return m.store.Close()
	}
	return nil
}

func (m *Module) Health(ctx context.Context) kernel.HealthStatus {
	if m.store == nil {
		return kernel.HealthStatus{Healthy: false, Message: "store not initialized"}
	}
	return kernel.HealthStatus{Healthy: true, Message: "curator running"}
}

// Store exposes the underlying store. It is here primarily for
// tests that want to read invocations directly.
func (m *Module) Store() *Store { return m.store }

func (m *Module) handleRecord(msg ipc.Message) (ipc.Message, error) {
	inv, ok := msg.Payload.(Invocation)
	if !ok {
		fmt.Fprintf(os.Stderr, "curator: expected Invocation, got %T\n", msg.Payload)
		return ipc.Message{}, nil
	}
	if err := m.store.Record(inv); err != nil {
		// We're a fire-and-forget subscriber — log and move on so
		// we never propagate latency or errors back to the agent.
		fmt.Fprintf(os.Stderr, "curator: record failed: %v\n", err)
	}
	return ipc.Message{}, nil
}

func (m *Module) handleScores(msg ipc.Message) (ipc.Message, error) {
	now := m.now()
	if filter, ok := msg.Payload.(ScoresFilter); ok && filter.SkillName != "" {
		invs, err := m.store.LoadInvocations(filter.SkillName, 0)
		if err != nil {
			return ipc.Message{}, err
		}
		score := ComputeScore(filter.SkillName, invs, now)
		return ipc.Message{
			Type:    ipc.MessageTypeResponse,
			Payload: []SkillScore{score},
		}, nil
	}
	scores, err := ComputeAllScores(m.store, now)
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: scores}, nil
}

func (m *Module) handleSuggestPrune(msg ipc.Message) (ipc.Message, error) {
	suggestions, err := ComputePruneSuggestions(m.store, m.now())
	if err != nil {
		return ipc.Message{}, err
	}
	if suggestions == nil {
		suggestions = []PruneSuggestion{}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: suggestions}, nil
}
