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

func (m *Module) Name() string                                         { return "auth" }
func (m *Module) Dependencies() []string                               { return nil }
func (m *Module) Init(ctx context.Context, svc *kernel.Services) error { m.bus = svc.Bus; return nil }
func (m *Module) Start(ctx context.Context) error {
	m.bus.Handle("auth", "auth.resolve", m.handleResolve)
	return nil
}
func (m *Module) Stop(ctx context.Context) error { return nil }
func (m *Module) Health(ctx context.Context) kernel.HealthStatus {
	return kernel.HealthStatus{Healthy: true, Message: "auth module running"}
}

// trustedResolveSources is the allowlist of internal modules permitted to ask
// the auth module to turn a raw credential into a Principal.
//
// TRUST BOUNDARY: auth.resolve performs credential->principal resolution and
// returns an authenticated identity. It must therefore only be reachable from
// trusted edge components that have already received the credential from an
// external client over a vetted transport (currently the "proxy", which
// terminates inbound HTTP and forwards bearer/API-key credentials). Allowing
// arbitrary in-process modules (e.g. agent tools or skills) to call this would
// let untrusted code mint principals from guessed/stolen credentials, so we
// fail closed on any unexpected msg.Source.
var trustedResolveSources = map[string]bool{
	"proxy": true,
}

func (m *Module) handleResolve(msg ipc.Message) (ipc.Message, error) {
	if !trustedResolveSources[msg.Source] {
		return ipc.Message{}, fmt.Errorf("auth.resolve: caller %q is not a trusted source", msg.Source)
	}
	req, ok := msg.Payload.(ResolveRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected ResolveRequest, got %T", msg.Payload)
	}
	principal, err := m.mapper.Resolve(req.Scheme, req.Credential)
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: principal}, nil
}
