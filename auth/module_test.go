package auth

import (
	"context"
	"testing"
	"time"
	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func TestAuthModuleImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Module)(nil)
}

func TestAuthModuleResolvesViaIPC(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	rbac := NewRBAC()
	mod := NewModule(sm, rbac)
	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)

	principal := Principal{Type: PrincipalUser, ID: "jane@corp.com", Tenant: "finance", Roles: []string{"admin"}}
	token, _ := sm.CreateToken(principal, 1*time.Hour)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "auth", Topic: "auth.resolve",
		Payload: ResolveRequest{Scheme: "bearer", Credential: token},
	})
	if err != nil { t.Fatalf("request: %v", err) }
	resolved, ok := resp.Payload.(Principal)
	if !ok { t.Fatalf("expected Principal, got %T", resp.Payload) }
	if resolved.ID != "jane@corp.com" { t.Fatalf("got %q", resolved.ID) }
	if len(resolved.Permissions) == 0 { t.Fatal("expected permissions") }
}

func TestAuthModuleHealthy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	mod := NewModule(NewSessionManager("test-secret-key-minimum-32-bytes!"), NewRBAC())
	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)
	if h := mod.Health(ctx); !h.Healthy { t.Fatalf("expected healthy: %s", h.Message) }
}
