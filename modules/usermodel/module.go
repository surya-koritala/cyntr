package usermodel

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Module exposes the user-profile store over the IPC bus.
type Module struct {
	bus   *ipc.Bus
	store *Store
}

// New constructs a Module backed by store. store must already be open.
func New(store *Store) *Module {
	return &Module{store: store}
}

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
