package auth

import "sync"

// RBAC manages roles and permission checking.
type RBAC struct {
	mu    sync.RWMutex
	roles map[string]Role
}

// NewRBAC creates an RBAC instance with the four built-in roles.
func NewRBAC() *RBAC {
	r := &RBAC{roles: make(map[string]Role)}

	r.roles["admin"] = Role{
		Name: "admin",
		Permissions: []Permission{
			PermManageTenants, PermManagePolicies, PermManageAgents, PermViewAllAudit,
		},
	}
	r.roles["team_lead"] = Role{
		Name: "team_lead",
		Permissions: []Permission{
			PermManageTeamAgents, PermManageTeamSkills, PermApproveActions, PermViewTeamAudit,
		},
	}
	r.roles["user"] = Role{
		Name: "user",
		Permissions: []Permission{
			PermInteractAgents, PermViewOwnAudit,
		},
	}
	r.roles["auditor"] = Role{
		Name: "auditor",
		Permissions: []Permission{
			PermViewAllAudit, PermExportAudit,
		},
	}

	return r
}

// AddRole registers a custom role. Overwrites if name exists.
func (r *RBAC) AddRole(role Role) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.roles[role.Name] = role
}

// RolePermissions returns the permissions for a role name.
func (r *RBAC) RolePermissions(name string) []Permission {
	r.mu.RLock()
	defer r.mu.RUnlock()

	role, ok := r.roles[name]
	if !ok {
		return nil
	}
	return role.Permissions
}

// HasPermission checks if a principal has a specific permission
// across any of their assigned roles.
func (r *RBAC) HasPermission(principal Principal, perm Permission) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, roleName := range principal.Roles {
		role, ok := r.roles[roleName]
		if !ok {
			continue
		}
		for _, p := range role.Permissions {
			if p == perm {
				return true
			}
		}
	}
	return false
}
