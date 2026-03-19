package auth

import "testing"

func TestRBACBuiltinRoles(t *testing.T) {
	rbac := NewRBAC()

	adminPerms := rbac.RolePermissions("admin")
	if len(adminPerms) == 0 {
		t.Fatal("expected admin to have permissions")
	}
	if !containsPerm(adminPerms, PermManageTenants) {
		t.Fatal("expected admin to have manage_tenants")
	}

	userPerms := rbac.RolePermissions("user")
	if !containsPerm(userPerms, PermInteractAgents) {
		t.Fatal("expected user to have interact_with_agents")
	}
	if containsPerm(userPerms, PermManageTenants) {
		t.Fatal("expected user NOT to have manage_tenants")
	}
}

func TestRBACCustomRole(t *testing.T) {
	rbac := NewRBAC()
	rbac.AddRole(Role{
		Name:        "viewer",
		Permissions: []Permission{PermViewAllAudit, PermExportAudit},
	})

	perms := rbac.RolePermissions("viewer")
	if len(perms) != 2 {
		t.Fatalf("expected 2 permissions, got %d", len(perms))
	}
}

func TestRBACHasPermission(t *testing.T) {
	rbac := NewRBAC()

	principal := Principal{
		Type:   PrincipalUser,
		ID:     "jane@corp.com",
		Tenant: "finance",
		Roles:  []string{"admin"},
	}

	if !rbac.HasPermission(principal, PermManageTenants) {
		t.Fatal("admin should have manage_tenants")
	}
	if !rbac.HasPermission(principal, PermViewAllAudit) {
		t.Fatal("admin should have view_all_audit")
	}
}

func TestRBACHasPermissionDenied(t *testing.T) {
	rbac := NewRBAC()

	principal := Principal{
		Type:   PrincipalUser,
		ID:     "bob@corp.com",
		Tenant: "marketing",
		Roles:  []string{"user"},
	}

	if rbac.HasPermission(principal, PermManageTenants) {
		t.Fatal("user should NOT have manage_tenants")
	}
}

func TestRBACMultipleRoles(t *testing.T) {
	rbac := NewRBAC()

	principal := Principal{
		Type:   PrincipalUser,
		ID:     "jane@corp.com",
		Tenant: "finance",
		Roles:  []string{"user", "auditor"},
	}

	if !rbac.HasPermission(principal, PermInteractAgents) {
		t.Fatal("expected interact_with_agents from user role")
	}
	if !rbac.HasPermission(principal, PermViewAllAudit) {
		t.Fatal("expected view_all_audit from auditor role")
	}
	if rbac.HasPermission(principal, PermManageTenants) {
		t.Fatal("should NOT have manage_tenants")
	}
}

func containsPerm(perms []Permission, p Permission) bool {
	for _, perm := range perms {
		if perm == p {
			return true
		}
	}
	return false
}
