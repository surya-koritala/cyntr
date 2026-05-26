package usermodel

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Module exposes the user-profile store over the IPC bus.
type Module struct {
	bus       *ipc.Bus
	store     *Store
	distiller *Distiller
}

// New constructs a Module backed by store. store must already be open.
func New(store *Store) *Module {
	return &Module{store: store}
}

// SetDistiller attaches the narrative distiller. When unset, the distill
// IPC topic returns an error and record_activity is still recorded but no
// background distillation runs.
func (m *Module) SetDistiller(d *Distiller) {
	m.distiller = d
}

// Store returns the underlying store, mainly so main.go can plumb it into
// the scheduler tick and HTTP handlers without re-opening the db.
func (m *Module) Store() *Store { return m.store }

// Distiller returns the configured distiller, if any.
func (m *Module) Distiller() *Distiller { return m.distiller }

func (m *Module) Name() string           { return "usermodel" }
func (m *Module) Dependencies() []string { return nil }

func (m *Module) Init(ctx context.Context, svc *kernel.Services) error {
	m.bus = svc.Bus
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	m.bus.Handle("usermodel", TopicGet, m.handleGet)
	m.bus.Handle("usermodel", TopicUpsertProfile, m.handleUpsertProfile)
	m.bus.Handle("usermodel", TopicUpsertPreferences, m.handleUpsertPreferences)
	m.bus.Handle("usermodel", TopicDistill, m.handleDistill)
	// record_activity is fire-and-forget — agents shouldn't wait on us to
	// finish writing the row before their chat returns to the user.
	m.bus.Subscribe("usermodel", TopicRecordActivity, m.handleRecordActivity)
	return nil
}

func (m *Module) Stop(ctx context.Context) error { return nil }

func (m *Module) Health(ctx context.Context) kernel.HealthStatus {
	if m.store == nil {
		return kernel.HealthStatus{Healthy: false, Message: "store not configured"}
	}
	return kernel.HealthStatus{Healthy: true, Message: "ok"}
}

// payloadMap pulls a map[string]string out of an IPC message payload, accepting
// either a typed map[string]string or a map[string]any (for cross-process IPC).
func payloadMap(payload any) (map[string]string, error) {
	switch v := payload.(type) {
	case map[string]string:
		return v, nil
	case map[string]any:
		out := make(map[string]string, len(v))
		for k, val := range v {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("usermodel: expected map[string]string payload, got %T", payload)
	}
}

func (m *Module) handleGet(msg ipc.Message) (ipc.Message, error) {
	args, err := payloadMap(msg.Payload)
	if err != nil {
		return ipc.Message{}, err
	}
	tenant := args["tenant"]
	user := args["user"]
	if tenant == "" || user == "" {
		return ipc.Message{}, fmt.Errorf("usermodel.get: tenant and user are required")
	}
	p, err := m.store.Get(tenant, user)
	if err == ErrNotFound {
		// Return an empty profile rather than an error — callers may treat
		// "no profile yet" as a normal cold-start case.
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: UserProfile{Tenant: tenant, User: user}}, nil
	}
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: p}, nil
}

func (m *Module) handleUpsertProfile(msg ipc.Message) (ipc.Message, error) {
	args, err := payloadMap(msg.Payload)
	if err != nil {
		return ipc.Message{}, err
	}
	tenant, user, md := args["tenant"], args["user"], args["md"]
	if tenant == "" || user == "" {
		return ipc.Message{}, fmt.Errorf("usermodel.upsert_profile: tenant and user are required")
	}
	if err := m.store.UpsertProfile(tenant, user, md); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (m *Module) handleUpsertPreferences(msg ipc.Message) (ipc.Message, error) {
	args, err := payloadMap(msg.Payload)
	if err != nil {
		return ipc.Message{}, err
	}
	tenant, user, md := args["tenant"], args["user"], args["md"]
	if tenant == "" || user == "" {
		return ipc.Message{}, fmt.Errorf("usermodel.upsert_preferences: tenant and user are required")
	}
	if err := m.store.UpsertPreferences(tenant, user, md); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

// handleDistill triggers a synchronous distillation pass for a single user.
// Returns a DistillResult so callers can surface size deltas and skip
// reasons. Manual triggers (this path) bypass the per-user rate limit —
// the assumption is that a human (or an explicit reset endpoint) is asking.
func (m *Module) handleDistill(msg ipc.Message) (ipc.Message, error) {
	args, err := payloadMap(msg.Payload)
	if err != nil {
		return ipc.Message{}, err
	}
	tenant, user := args["tenant"], args["user"]
	if tenant == "" || user == "" {
		return ipc.Message{}, fmt.Errorf("usermodel.distill: tenant and user are required")
	}
	if m.distiller == nil {
		return ipc.Message{}, fmt.Errorf("usermodel.distill: distiller not configured")
	}
	// Manual triggers use the foreground context — the IPC bus already
	// enforces its own timeout on the round-trip. Distiller.DistillUserForce
	// applies a 30s inner timeout to the LLM call.
	res, _ := m.distiller.DistillUserForce(context.Background(), tenant, user)
	if res == nil {
		res = &DistillResult{Tenant: tenant, User: user, Error: "no result"}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: *res}, nil
}

// handleRecordActivity is the fire-and-forget sink for chat-level activity
// summaries. Errors are swallowed — the chat path must never depend on the
// usermodel database being writable.
func (m *Module) handleRecordActivity(msg ipc.Message) (ipc.Message, error) {
	args, err := payloadMap(msg.Payload)
	if err != nil {
		return ipc.Message{}, nil
	}
	m.store.RecordActivity(args["tenant"], args["user"], args["summary"])
	return ipc.Message{}, nil
}
