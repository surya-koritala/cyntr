package tenant

import (
	"testing"
	"github.com/cyntr-dev/cyntr/kernel/config"
	"github.com/cyntr-dev/cyntr/kernel/resource"
)

func TestTenantManagerLoadFromConfig(t *testing.T) {
	cfg := config.CyntrConfig{
		Tenants: map[string]config.TenantConfig{
			"finance":   {Isolation: "process", Policy: "finance-strict"},
			"marketing": {Isolation: "namespace", Policy: "marketing-standard"},
		},
	}
	rm := resource.NewManager()
	tm, err := NewManager(cfg, rm)
	if err != nil { t.Fatalf("new manager: %v", err) }
	tenants := tm.List()
	if len(tenants) != 2 { t.Fatalf("expected 2, got %d", len(tenants)) }
}

func TestTenantManagerGet(t *testing.T) {
	cfg := config.CyntrConfig{Tenants: map[string]config.TenantConfig{"finance": {Isolation: "process", Policy: "finance-strict"}}}
	rm := resource.NewManager()
	tm, _ := NewManager(cfg, rm)
	tenant, ok := tm.Get("finance")
	if !ok { t.Fatal("expected finance") }
	if tenant.Name != "finance" { t.Fatalf("got %q", tenant.Name) }
	if tenant.Isolation != IsolationProcess { t.Fatalf("got %s", tenant.Isolation) }
	if tenant.Policy != "finance-strict" { t.Fatalf("got %q", tenant.Policy) }
}

func TestTenantManagerGetNotFound(t *testing.T) {
	cfg := config.CyntrConfig{Tenants: map[string]config.TenantConfig{}}
	rm := resource.NewManager()
	tm, _ := NewManager(cfg, rm)
	_, ok := tm.Get("nonexistent")
	if ok { t.Fatal("expected not found") }
}

func TestTenantManagerCreate(t *testing.T) {
	cfg := config.CyntrConfig{Tenants: map[string]config.TenantConfig{}}
	rm := resource.NewManager()
	tm, _ := NewManager(cfg, rm)
	if err := tm.Create("devops", IsolationNamespace, "devops-policy"); err != nil { t.Fatalf("create: %v", err) }
	tenant, ok := tm.Get("devops")
	if !ok { t.Fatal("expected devops") }
	if tenant.Isolation != IsolationNamespace { t.Fatalf("got %s", tenant.Isolation) }
}

func TestTenantManagerCreateDuplicate(t *testing.T) {
	cfg := config.CyntrConfig{Tenants: map[string]config.TenantConfig{"finance": {Isolation: "namespace"}}}
	rm := resource.NewManager()
	tm, _ := NewManager(cfg, rm)
	if err := tm.Create("finance", IsolationNamespace, ""); err == nil { t.Fatal("expected error") }
}

func TestTenantManagerDelete(t *testing.T) {
	cfg := config.CyntrConfig{Tenants: map[string]config.TenantConfig{"temp": {Isolation: "namespace"}}}
	rm := resource.NewManager()
	tm, _ := NewManager(cfg, rm)
	if err := tm.Delete("temp"); err != nil { t.Fatalf("delete: %v", err) }
	_, ok := tm.Get("temp")
	if ok { t.Fatal("expected deleted") }
}

func TestTenantManagerDeleteNotFound(t *testing.T) {
	cfg := config.CyntrConfig{Tenants: map[string]config.TenantConfig{}}
	rm := resource.NewManager()
	tm, _ := NewManager(cfg, rm)
	if err := tm.Delete("nonexistent"); err == nil { t.Fatal("expected error") }
}

func TestIsolationModeString(t *testing.T) {
	tests := []struct{ mode IsolationMode; want string }{
		{IsolationNamespace, "namespace"}, {IsolationProcess, "process"}, {IsolationMode(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want { t.Errorf("got %q, want %q", got, tt.want) }
	}
}
