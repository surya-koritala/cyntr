package auth

import "fmt"

type IdentityMapper struct {
	sessions *SessionManager
	rbac     *RBAC
}

func NewIdentityMapper(sessions *SessionManager, rbac *RBAC) *IdentityMapper {
	return &IdentityMapper{sessions: sessions, rbac: rbac}
}

func (m *IdentityMapper) Resolve(scheme, credential string) (Principal, error) {
	var p Principal
	var err error
	switch scheme {
	case "bearer":
		p, err = m.sessions.ValidateToken(credential)
	case "apikey":
		p, err = m.sessions.ValidateAPIKey(credential)
	default:
		return Principal{}, fmt.Errorf("unsupported auth scheme: %q", scheme)
	}
	if err != nil { return Principal{}, fmt.Errorf("auth failed: %w", err) }
	p.Permissions = m.resolvePermissions(p)
	return p, nil
}

func (m *IdentityMapper) resolvePermissions(p Principal) []Permission {
	seen := make(map[Permission]bool)
	var perms []Permission
	for _, roleName := range p.Roles {
		for _, perm := range m.rbac.RolePermissions(roleName) {
			if !seen[perm] { seen[perm] = true; perms = append(perms, perm) }
		}
	}
	return perms
}
