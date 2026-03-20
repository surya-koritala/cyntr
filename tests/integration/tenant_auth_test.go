package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/auth"
	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/tenant"
)

func TestTenantAuthPolicyFlow(t *testing.T) {
	dir := t.TempDir()

	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte(`
rules:
  - name: finance-deny-shell
    tenant: finance
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 30
  - name: allow-all
    tenant: "*"
    action: "*"
    tool: "*"
    agent: "*"
    decision: allow
    priority: 1
`), 0644)

	cfgPath := filepath.Join(dir, "cyntr.yaml")
	os.WriteFile(cfgPath, []byte(`
version: "1"
listen:
  address: "127.0.0.1:8080"
tenants:
  finance:
    isolation: process
    policy: finance-strict
  marketing:
    isolation: namespace
    policy: marketing-standard
`), 0644)

	k := kernel.New()
	if err := k.LoadConfig(cfgPath); err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Create tenant manager
	cfgStore := k.Config()
	rm := k.ResourceManager()
	tm, err := tenant.NewManager(cfgStore.Get(), rm)
	if err != nil { t.Fatalf("tenant manager: %v", err) }

	tenants := tm.List()
	if len(tenants) != 2 { t.Fatalf("expected 2 tenants, got %d", len(tenants)) }

	fin, ok := tm.Get("finance")
	if !ok { t.Fatal("expected finance") }
	if fin.Isolation != tenant.IsolationProcess { t.Fatalf("expected process, got %s", fin.Isolation) }

	// Auth components
	sm := auth.NewSessionManager("test-secret-key-minimum-32-bytes!")
	rbac := auth.NewRBAC()
	authMod := auth.NewModule(sm, rbac)
	policyEngine := policy.NewEngine(policyPath)

	if err := k.Register(policyEngine); err != nil { t.Fatalf("register policy: %v", err) }
	if err := k.Register(authMod); err != nil { t.Fatalf("register auth: %v", err) }

	ctx := context.Background()
	if err := k.Start(ctx); err != nil { t.Fatalf("start: %v", err) }
	defer k.Stop(ctx)

	bus := k.Bus()

	// Create JWT for finance admin
	token, _ := sm.CreateToken(auth.Principal{
		Type: auth.PrincipalUser, ID: "jane@corp.com", Tenant: "finance", Roles: []string{"admin"},
	}, 1*time.Hour)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Resolve via IPC
	authResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "auth", Topic: "auth.resolve",
		Payload: auth.ResolveRequest{Scheme: "bearer", Credential: token},
	})
	if err != nil { t.Fatalf("auth resolve: %v", err) }

	resolved := authResp.Payload.(auth.Principal)
	if resolved.ID != "jane@corp.com" { t.Fatalf("got %q", resolved.ID) }
	if !rbac.HasPermission(resolved, auth.PermManageTenants) { t.Fatal("expected manage_tenants") }

	// Policy check: finance+shell should be denied
	policyResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "policy", Topic: "policy.check",
		Payload: policy.CheckRequest{Tenant: resolved.Tenant, Action: "tool_call", Tool: "shell_exec", Agent: "assistant", User: resolved.ID},
	})
	if err != nil { t.Fatalf("policy check: %v", err) }

	checkResp := policyResp.Payload.(policy.CheckResponse)
	if checkResp.Decision != policy.Deny { t.Fatalf("expected deny, got %s", checkResp.Decision) }
}
