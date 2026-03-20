package auth

import (
	"context"
	"fmt"
	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

type ResolveRequest struct {
	Scheme     string
	Credential string
}

type Module struct {
	mapper *IdentityMapper
	bus    *ipc.Bus
}

func NewModule(sm *SessionManager, rbac *RBAC) *Module {
	return &Module{mapper: NewIdentityMapper(sm, rbac)}
}

func (m *Module) Name() string           { return "auth" }
func (m *Module) Dependencies() []string { return nil }
func (m *Module) Init(ctx context.Context, svc *kernel.Services) error { m.bus = svc.Bus; return nil }
func (m *Module) Start(ctx context.Context) error { m.bus.Handle("auth", "auth.resolve", m.handleResolve); return nil }
func (m *Module) Stop(ctx context.Context) error { return nil }
func (m *Module) Health(ctx context.Context) kernel.HealthStatus { return kernel.HealthStatus{Healthy: true, Message: "auth module running"} }

func (m *Module) handleResolve(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(ResolveRequest)
	if !ok { return ipc.Message{}, fmt.Errorf("expected ResolveRequest, got %T", msg.Payload) }
	principal, err := m.mapper.Resolve(req.Scheme, req.Credential)
	if err != nil { return ipc.Message{}, err }
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: principal}, nil
}
