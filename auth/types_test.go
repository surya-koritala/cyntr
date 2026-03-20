package auth

import "testing"

func TestPrincipalIsUser(t *testing.T) {
	p := Principal{Type: PrincipalUser, ID: "jane@corp.com", Tenant: "finance"}
	if !p.IsUser() {
		t.Fatal("expected user")
	}
	if p.IsAgent() {
		t.Fatal("expected not agent")
	}
}

func TestPrincipalIsAgent(t *testing.T) {
	p := Principal{Type: PrincipalAgent, ID: "finance-analyst", Tenant: "finance"}
	if !p.IsAgent() {
		t.Fatal("expected agent")
	}
	if p.IsUser() {
		t.Fatal("expected not user")
	}
}

func TestPermissionString(t *testing.T) {
	tests := []struct {
		p    Permission
		want string
	}{
		{PermManageTenants, "manage_tenants"},
		{PermInteractAgents, "interact_with_agents"},
		{PermViewAllAudit, "view_all_audit"},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}
