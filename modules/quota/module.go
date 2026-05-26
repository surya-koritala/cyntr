package quota

import (
	"context"
	"fmt"
	"sync"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/log"
)

var logger = log.Default().WithModule("quota")

// ModuleName is the canonical name of the quota module on the IPC bus.
const ModuleName = "quota"

// IPC topic names.
const (
	TopicCheck        = "quota.check"
	TopicRecord       = "quota.record"
	TopicConfigSet    = "quota.config.set"
	TopicConfigGet    = "quota.config.get"
	TopicUsage        = "quota.usage"
	TopicSlotAcquire  = "quota.slot.acquire"
	TopicSlotRelease  = "quota.slot.release"
	TopicSessionStart = "quota.session.start"
)

// Module is the kernel module implementation for tenant quotas.
type Module struct {
	dbPath   string
	enforcer *Enforcer
	store    *Store
	bus      *ipc.Bus

	slotMu      sync.Mutex
	slotCounter uint64
	slots       map[string]func() // slotID -> release
}

// New constructs a Module. dbPath is the SQLite path used for persistence; pass
// the empty string to disable persistence (in-memory only).
func New(dbPath string) *Module {
	if dbPath == "" {
		dbPath = "quota.db"
	}
	return &Module{dbPath: dbPath, slots: make(map[string]func())}
}

// Enforcer returns the live Enforcer (useful in tests and for direct callers).
func (m *Module) Enforcer() *Enforcer { return m.enforcer }

func (m *Module) Name() string           { return ModuleName }
func (m *Module) Dependencies() []string { return nil }

func (m *Module) Init(ctx context.Context, svc *kernel.Services) error {
	m.bus = svc.Bus
	store, err := NewStore(m.dbPath)
	if err != nil {
		return fmt.Errorf("quota: open store: %w", err)
	}
	m.store = store
	m.enforcer = NewEnforcer(store)
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	m.bus.Handle(ModuleName, TopicCheck, m.handleCheck)
	m.bus.Handle(ModuleName, TopicRecord, m.handleRecord)
	m.bus.Handle(ModuleName, TopicConfigSet, m.handleConfigSet)
	m.bus.Handle(ModuleName, TopicConfigGet, m.handleConfigGet)
	m.bus.Handle(ModuleName, TopicUsage, m.handleUsage)
	m.bus.Handle(ModuleName, TopicSlotAcquire, m.handleSlotAcquire)
	m.bus.Handle(ModuleName, TopicSlotRelease, m.handleSlotRelease)
	m.bus.Handle(ModuleName, TopicSessionStart, m.handleSessionStart)
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	if m.store != nil {
		return m.store.Close()
	}
	return nil
}

func (m *Module) Health(ctx context.Context) kernel.HealthStatus {
	if m.enforcer == nil {
		return kernel.HealthStatus{Healthy: false, Message: "enforcer not initialised"}
	}
	return kernel.HealthStatus{Healthy: true, Message: "quota enforcer ready"}
}

// handleCheck answers quota.check requests.
//
// The check is non-mutating for the token dimension (use quota.record to debit
// after a successful LLM call) and acquires/releases nothing for concurrency
// (callers should use Enforcer.AcquireAgentSlot directly to obtain a release
// callback). Rate-bucket calls are mutating: each successful quota.check for
// kind=rate consumes one token from the bucket.
func (m *Module) handleCheck(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(CheckRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("quota.check: expected CheckRequest, got %T", msg.Payload)
	}
	cfg := m.enforcer.GetConfig(req.Tenant)
	resp := CheckResponse{Allowed: true}

	var err error
	switch req.Kind {
	case KindTokens:
		err = m.enforcer.CheckTokens(req.Tenant, req.Amount)
		resp.Limit = cfg.TokensPerDay
	case KindRate:
		err = m.enforcer.CheckRate(req.Tenant)
		resp.Limit = int64(cfg.RequestsPerMinute)
	case KindConcurrency:
		// Reflect current usage without acquiring; callers that need a slot
		// must call AcquireAgentSlot directly.
		usage := m.enforcer.CurrentUsage(req.Tenant)
		resp.Current = int64(usage.ActiveAgents)
		resp.Limit = int64(cfg.MaxConcurrentAgents)
		if cfg.MaxConcurrentAgents > 0 && usage.ActiveAgents >= cfg.MaxConcurrentAgents {
			err = &ErrQuotaExceeded{
				Tenant: req.Tenant, Kind: KindConcurrency,
				Limit: int64(cfg.MaxConcurrentAgents), Current: int64(usage.ActiveAgents),
			}
		}
	case KindSessions:
		usage := m.enforcer.CurrentUsage(req.Tenant)
		resp.Current = usage.SessionsToday
		resp.Limit = int64(cfg.MaxSessionsPerDay)
		if cfg.MaxSessionsPerDay > 0 && usage.SessionsToday >= int64(cfg.MaxSessionsPerDay) {
			err = &ErrQuotaExceeded{
				Tenant: req.Tenant, Kind: KindSessions,
				Limit: int64(cfg.MaxSessionsPerDay), Current: usage.SessionsToday,
				ResetAt: endOfUTCDay(nowFn()),
			}
		}
	default:
		return ipc.Message{}, fmt.Errorf("quota.check: unknown kind %q", req.Kind)
	}

	if qe, isQuotaErr := err.(*ErrQuotaExceeded); isQuotaErr {
		resp.Allowed = false
		resp.Current = qe.Current
		resp.Limit = qe.Limit
		resp.ResetAt = qe.ResetAt
		resp.Reason = qe.Error()
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: resp}, nil
}

// handleRecord is the fire-and-forget token-debit handler. It also surfaces a
// trivial response for callers that prefer Request semantics.
func (m *Module) handleRecord(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(RecordRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("quota.record: expected RecordRequest, got %T", msg.Payload)
	}
	switch req.Kind {
	case KindTokens:
		m.enforcer.RecordTokens(req.Tenant, req.Amount)
	case KindSessions:
		if err := m.enforcer.RecordSession(req.Tenant); err != nil {
			logger.Warn("session quota breached during record", map[string]any{
				"tenant": req.Tenant, "error": err.Error(),
			})
		}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: map[string]any{"ok": true}}, nil
}

func (m *Module) handleConfigSet(msg ipc.Message) (ipc.Message, error) {
	cfg, ok := msg.Payload.(QuotaConfig)
	if !ok {
		return ipc.Message{}, fmt.Errorf("quota.config.set: expected QuotaConfig, got %T", msg.Payload)
	}
	if err := m.enforcer.SetConfig(cfg); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: cfg}, nil
}

func (m *Module) handleConfigGet(msg ipc.Message) (ipc.Message, error) {
	tenant, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("quota.config.get: expected tenant string, got %T", msg.Payload)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: m.enforcer.GetConfig(tenant)}, nil
}

// handleSlotAcquire reserves a concurrent-agent slot and returns a slotID
// callers later pass to TopicSlotRelease.
func (m *Module) handleSlotAcquire(msg ipc.Message) (ipc.Message, error) {
	tenant, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("quota.slot.acquire: expected tenant string, got %T", msg.Payload)
	}
	release, err := m.enforcer.AcquireAgentSlot(tenant)
	if err != nil {
		if qe, isQE := err.(*ErrQuotaExceeded); isQE {
			return ipc.Message{Type: ipc.MessageTypeResponse, Payload: SlotResponse{
				Allowed: false, Reason: qe.Error(),
			}}, nil
		}
		return ipc.Message{}, err
	}

	m.slotMu.Lock()
	m.slotCounter++
	slotID := fmt.Sprintf("%s-%d", tenant, m.slotCounter)
	m.slots[slotID] = release
	m.slotMu.Unlock()
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: SlotResponse{
		Allowed: true, SlotID: slotID,
	}}, nil
}

// handleSlotRelease releases a previously acquired concurrent-agent slot.
func (m *Module) handleSlotRelease(msg ipc.Message) (ipc.Message, error) {
	slotID, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("quota.slot.release: expected slotID string, got %T", msg.Payload)
	}
	m.slotMu.Lock()
	release, found := m.slots[slotID]
	if found {
		delete(m.slots, slotID)
	}
	m.slotMu.Unlock()
	if found {
		release()
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: map[string]any{"ok": found}}, nil
}

// handleSessionStart records a new session and returns a CheckResponse-like
// payload indicating whether the cap has been exceeded.
func (m *Module) handleSessionStart(msg ipc.Message) (ipc.Message, error) {
	tenant, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("quota.session.start: expected tenant string, got %T", msg.Payload)
	}
	cfg := m.enforcer.GetConfig(tenant)
	err := m.enforcer.RecordSession(tenant)
	resp := CheckResponse{Allowed: true, Limit: int64(cfg.MaxSessionsPerDay)}
	if qe, isQE := err.(*ErrQuotaExceeded); isQE {
		resp.Allowed = false
		resp.Reason = qe.Error()
		resp.Current = qe.Current
		resp.Limit = qe.Limit
		resp.ResetAt = qe.ResetAt
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: resp}, nil
}

func (m *Module) handleUsage(msg ipc.Message) (ipc.Message, error) {
	tenant, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("quota.usage: expected tenant string, got %T", msg.Payload)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: m.enforcer.CurrentUsage(tenant)}, nil
}
