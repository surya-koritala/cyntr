package auth

import "testing"

func TestIdentityMapperResolveJWT(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	rbac := NewRBAC()
	mapper := NewIdentityMapper(sm, rbac)
	principal := Principal{Type: PrincipalUser, ID: "jane@corp.com", Tenant: "finance", Roles: []string{"admin"}}
	token, _ := sm.CreateToken(principal, 3600_000_000_000)
	resolved, err := mapper.Resolve("bearer", token)
	if err != nil { t.Fatalf("resolve: %v", err) }
	if resolved.ID != "jane@corp.com" { t.Fatalf("got %q", resolved.ID) }
	if len(resolved.Permissions) == 0 { t.Fatal("expected permissions") }
}

func TestIdentityMapperResolveAPIKey(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	rbac := NewRBAC()
	mapper := NewIdentityMapper(sm, rbac)
	principal := Principal{Type: PrincipalUser, ID: "ci-bot", Tenant: "devops", Roles: []string{"admin"}}
	key, _ := sm.CreateAPIKey("ci-deploy", principal)
	resolved, err := mapper.Resolve("apikey", key)
	if err != nil { t.Fatalf("resolve: %v", err) }
	if resolved.ID != "ci-bot" { t.Fatalf("got %q", resolved.ID) }
}

func TestIdentityMapperResolveUnknownScheme(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	rbac := NewRBAC()
	mapper := NewIdentityMapper(sm, rbac)
	_, err := mapper.Resolve("oauth2", "token")
	if err == nil { t.Fatal("expected error") }
}
