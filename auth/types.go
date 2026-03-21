package auth

// PrincipalType distinguishes users from agents.
type PrincipalType int

const (
	PrincipalUser  PrincipalType = iota
	PrincipalAgent
)

// Scope constants for API key authorization.
const (
	ScopeRead  = "read"
	ScopeAgent = "agent"
	ScopeAdmin = "admin"
)

// Principal represents an authenticated identity in the system.
type Principal struct {
	Type        PrincipalType
	ID          string       // user email or agent name
	Tenant      string       // which tenant this principal belongs to
	Roles       []string     // role names assigned to this principal
	Permissions []Permission // resolved permissions from roles
	Scopes      []string     // API key scopes (read, agent, admin)
}

func (p Principal) IsUser() bool  { return p.Type == PrincipalUser }
func (p Principal) IsAgent() bool { return p.Type == PrincipalAgent }

// Permission represents a single capability in the RBAC system.
type Permission string

const (
	PermManageTenants   Permission = "manage_tenants"
	PermManagePolicies  Permission = "manage_policies"
	PermManageAgents    Permission = "manage_agents"
	PermViewAllAudit    Permission = "view_all_audit"
	PermManageTeamAgents Permission = "manage_team_agents"
	PermManageTeamSkills Permission = "manage_team_skills"
	PermApproveActions  Permission = "approve_actions"
	PermViewTeamAudit   Permission = "view_team_audit"
	PermInteractAgents  Permission = "interact_with_agents"
	PermViewOwnAudit    Permission = "view_own_audit"
	PermExportAudit     Permission = "export_audit"
)

func (p Permission) String() string { return string(p) }

// Role defines a named set of permissions.
type Role struct {
	Name        string       `yaml:"name"`
	Permissions []Permission `yaml:"permissions"`
}

// RoleConfig is the top-level YAML structure for role definitions.
type RoleConfig struct {
	Roles []Role `yaml:"roles"`
}
