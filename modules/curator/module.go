package curator

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// IPC topics owned by the curator module.
const (
	TopicRecord           = "curator.record"
	TopicScores           = "curator.scores"
	TopicSuggestPrune     = "curator.suggest_prune"
	TopicJudge            = "curator.judge"
	TopicPruneRun         = "curator.prune.run"
	TopicConsolidateRun   = "curator.consolidate.run"
	TopicImproveRun       = "curator.improve.run"
	ModuleName            = "curator"
)

// Module is the kernel-facing wrapper around the curator store +
// score logic. v0 exposed record / scores / suggest_prune; v1 adds
// the judge, the auto-prune action, and consolidation suggestions.
type Module struct {
	dbPath string
	bus    *ipc.Bus
	store  *Store
	sub    *ipc.Subscription
	now    func() time.Time // injectable for tests

	// v1 additions:
	judge       *Judge
	disabler    PruneSkillDisabler
	snapshotter ConsolidationSnapshotter
	improver    *Improver // A3: proposes improved versions of failing skills

	// Background prune loop. Cancel via stopCancel during Stop.
	stopMu     sync.Mutex
	stopCancel context.CancelFunc
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

// SetJudge wires the LLM judge. If unset (the default), curator.judge
// IPC + the POST /curator/judge endpoint return an error. This keeps
// judge calls strictly opt-in — operators must explicitly construct
// the Judge with their provider of choice.
func (m *Module) SetJudge(j *Judge) {
	m.judge = j
}

// Judge exposes the wired judge for tests.
func (m *Module) Judge() *Judge { return m.judge }

// SetImprover wires the skill improver (A3). When unset, curator.improve.run
// returns an error — self-improvement is opt-in.
func (m *Module) SetImprover(im *Improver) { m.improver = im }

// handleImproveRun proposes improved versions of failing skills. With a
// non-empty string payload it improves that one skill; otherwise it scans all
// currently-failing skills. Returns the list of skill names a proposal was
// raised for.
func (m *Module) handleImproveRun(msg ipc.Message) (ipc.Message, error) {
	if m.improver == nil {
		return ipc.Message{}, fmt.Errorf("curator.improve.run: improver not configured")
	}
	if name, ok := msg.Payload.(string); ok && name != "" {
		proposed, err := m.improver.Improve(context.Background(), m.store, name)
		if err != nil {
			return ipc.Message{}, err
		}
		var out []string
		if proposed {
			out = []string{name}
		}
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: out}, nil
	}
	improved, err := m.runImproveScan(context.Background())
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: improved}, nil
}

// runImproveScan proposes improvements for every currently-failing skill and
// returns the names a proposal was raised for.
func (m *Module) runImproveScan(ctx context.Context) ([]string, error) {
	scores, err := ComputeAllScores(m.store, m.now())
	if err != nil {
		return nil, err
	}
	var improved []string
	for _, s := range scores {
		if s.Health != HealthFailing {
			continue
		}
		proposed, err := m.improver.Improve(ctx, m.store, s.SkillName)
		if err != nil {
			continue // best-effort per skill
		}
		if proposed {
			improved = append(improved, s.SkillName)
		}
	}
	return improved, nil
}

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
	m.bus.Handle(ModuleName, TopicJudge, m.handleJudge)
	m.bus.Handle(ModuleName, TopicPruneRun, m.handlePruneRun)
	m.bus.Handle(ModuleName, TopicConsolidateRun, m.handleConsolidateRun)
	m.bus.Handle(ModuleName, TopicImproveRun, m.handleImproveRun)

	// Spin up the background auto-prune loop. We use a goroutine
	// rather than the heavyweight scheduler module to keep this
	// self-contained and because the curator owns the action.
	loopCtx, cancel := context.WithCancel(context.Background())
	m.stopMu.Lock()
	m.stopCancel = cancel
	m.stopMu.Unlock()
	go m.pruneLoop(loopCtx)
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	m.stopMu.Lock()
	if m.stopCancel != nil {
		m.stopCancel()
		m.stopCancel = nil
	}
	m.stopMu.Unlock()

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

// SetNow overrides the module's clock. Used by tests to pin the
// "now" timestamp so prune / score computations are deterministic.
func (m *Module) SetNow(fn func() time.Time) { m.now = fn }

// JudgeEnabled returns true iff a Judge has been wired AND the
// CYNTR_CURATOR_JUDGE env var opts in. We keep both gates so a
// misconfigured deploy doesn't accidentally start burning tokens.
func (m *Module) JudgeEnabled() bool {
	return m.judge != nil && os.Getenv("CYNTR_CURATOR_JUDGE") == "1"
}

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

// handleJudge runs a single judgment via the wired LLM provider. We
// honour the rate-limit gate even on direct calls so an over-eager
// operator hitting the API can't tear through token budget.
func (m *Module) handleJudge(msg ipc.Message) (ipc.Message, error) {
	inv, ok := msg.Payload.(InvocationContext)
	if !ok {
		return ipc.Message{}, fmt.Errorf("curator.judge: expected InvocationContext, got %T", msg.Payload)
	}
	if m.judge == nil {
		return ipc.Message{}, fmt.Errorf("curator.judge: no judge configured (set CYNTR_CURATOR_JUDGE and wire a provider)")
	}
	// Rate-limit check, unless caller passed an explicit
	// InvocationID — that path is an explicit re-score and the
	// operator has accepted the cost.
	if inv.InvocationID == 0 {
		ok, err := m.judge.ShouldJudge(m.store, inv.SkillName)
		if err != nil {
			return ipc.Message{}, err
		}
		if !ok {
			return ipc.Message{}, fmt.Errorf("curator.judge: rate-limited (wait for more invocations of %q)", inv.SkillName)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	result, err := m.judge.JudgeAndPersist(ctx, m.store, inv)
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: result}, nil
}

// handlePruneRun runs one auto-prune pass on demand. The background
// loop runs it on a cadence; this lets ops trigger it manually too.
func (m *Module) handlePruneRun(msg ipc.Message) (ipc.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	report, err := m.PruneFailingSkills(ctx)
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: report}, nil
}

// handleConsolidateRun returns the current consolidation suggestions
// without performing any action.
func (m *Module) handleConsolidateRun(msg ipc.Message) (ipc.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	report, err := m.SuggestConsolidation(ctx)
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: report}, nil
}

// pruneLoop drives the auto-prune action on a configurable cadence.
// Default 24h; CYNTR_CURATOR_PRUNE_INTERVAL overrides for tests /
// staging. The loop exits cleanly when Stop is called.
func (m *Module) pruneLoop(ctx context.Context) {
	interval := pruneCadenceFromEnv()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := m.PruneFailingSkills(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "curator: prune loop: %v\n", err)
			}
		}
	}
}

