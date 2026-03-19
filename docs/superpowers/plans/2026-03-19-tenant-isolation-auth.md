# Tenant Isolation + Auth/Identity Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the tenant management system (CRUD, resource limits, isolation mode tracking) and auth/identity infrastructure (principals, RBAC with permissions, JWT sessions, API key auth) as kernel-integrated components.

**Architecture:** Two packages. `tenant/` manages tenant lifecycle — creates tenants from config, applies resource limits via the existing ResourceManager, and tracks isolation mode. `auth/` defines principals (users + agents), roles with permissions, and provides session management via JWTs and API keys. An Auth module implements `kernel.Module` and handles `auth.resolve` IPC requests to map credentials to principals. Both integrate with existing kernel config and IPC bus.

**Tech Stack:** Go 1.22+, `golang.org/x/crypto` for bcrypt (API key hashing), stdlib `crypto/hmac` for JWT signing (HS256). No external auth dependencies — real OIDC/SAML comes in a later plan.

**Spec:** `docs/superpowers/specs/2026-03-19-cyntr-enterprise-platform-design.md` (Sections 3, 4)

**Dependencies:** Kernel + Policy Engine + Audit Logger (Plans 1-2).

**Deferred to future plans:**
- Process isolation (OS processes, cgroups, Unix domain sockets) — requires Plan 4 (Agent Runtime) to have agents to isolate.
- Real OIDC/SAML integration — requires external IdP. This plan builds the local auth framework that OIDC/SAML will plug into.
- Channel identity binding (Slack/Teams user mapping) — comes with Plan 6 (Channel Manager).
- mTLS for federation peers — comes with Plan 8 (Federation).

---

## File Structure

```
tenant/
├── tenant.go              # Tenant struct, TenantManager: CRUD, load from config
├── tenant_test.go          # Tenant management tests with real config
auth/
├── types.go               # Principal, Role, Permission, RoleConfig
├── rbac.go                # RBAC: check permissions, load roles from YAML
├── session.go             # SessionManager: JWT creation/validation, API keys
├── identity.go            # IdentityMapper: resolve credentials to principals
├── module.go              # Auth kernel module: IPC handler for auth.resolve
├── types_test.go          # Principal/Role type tests
├── rbac_test.go           # Permission checking tests
├── session_test.go        # JWT and API key tests
├── identity_test.go       # Identity resolution tests
├── module_test.go         # Auth module IPC tests
```

---

## Chunk 1: Tenant Manager

### Task 1: Implement Tenant Manager

**Files:**
- Create: `tenant/tenant.go`
- Create: `tenant/tenant_test.go`

- [ ] **Step 1: Write failing tests**

Create `tenant/tenant_test.go`:
```go
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
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	tenants := tm.List()
	if len(tenants) != 2 {
		t.Fatalf("expected 2 tenants, got %d", len(tenants))
	}
}

func TestTenantManagerGet(t *testing.T) {
	cfg := config.CyntrConfig{
		Tenants: map[string]config.TenantConfig{
			"finance": {Isolation: "process", Policy: "finance-strict"},
		},
	}

	rm := resource.NewManager()
	tm, _ := NewManager(cfg, rm)

	tenant, ok := tm.Get("finance")
	if !ok {
		t.Fatal("expected to find 'finance'")
	}
	if tenant.Name != "finance" {
		t.Fatalf("expected name 'finance', got %q", tenant.Name)
	}
	if tenant.Isolation != IsolationProcess {
		t.Fatalf("expected process isolation, got %s", tenant.Isolation)
	}
	if tenant.Policy != "finance-strict" {
		t.Fatalf("expected policy 'finance-strict', got %q", tenant.Policy)
	}
}

func TestTenantManagerGetNotFound(t *testing.T) {
	cfg := config.CyntrConfig{Tenants: map[string]config.TenantConfig{}}
	rm := resource.NewManager()
	tm, _ := NewManager(cfg, rm)

	_, ok := tm.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestTenantManagerCreate(t *testing.T) {
	cfg := config.CyntrConfig{Tenants: map[string]config.TenantConfig{}}
	rm := resource.NewManager()
	tm, _ := NewManager(cfg, rm)

	err := tm.Create("devops", IsolationNamespace, "devops-policy")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	tenant, ok := tm.Get("devops")
	if !ok {
		t.Fatal("expected to find 'devops'")
	}
	if tenant.Isolation != IsolationNamespace {
		t.Fatalf("expected namespace, got %s", tenant.Isolation)
	}
}

func TestTenantManagerCreateDuplicate(t *testing.T) {
	cfg := config.CyntrConfig{
		Tenants: map[string]config.TenantConfig{
			"finance": {Isolation: "namespace"},
		},
	}
	rm := resource.NewManager()
	tm, _ := NewManager(cfg, rm)

	err := tm.Create("finance", IsolationNamespace, "")
	if err == nil {
		t.Fatal("expected error for duplicate")
	}
}

func TestTenantManagerDelete(t *testing.T) {
	cfg := config.CyntrConfig{
		Tenants: map[string]config.TenantConfig{
			"temp": {Isolation: "namespace"},
		},
	}
	rm := resource.NewManager()
	tm, _ := NewManager(cfg, rm)

	err := tm.Delete("temp")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, ok := tm.Get("temp")
	if ok {
		t.Fatal("expected tenant to be deleted")
	}
}

func TestTenantManagerDeleteNotFound(t *testing.T) {
	cfg := config.CyntrConfig{Tenants: map[string]config.TenantConfig{}}
	rm := resource.NewManager()
	tm, _ := NewManager(cfg, rm)

	err := tm.Delete("nonexistent")
	if err == nil {
		t.Fatal("expected error for deleting nonexistent tenant")
	}
}

func TestIsolationModeString(t *testing.T) {
	tests := []struct {
		mode IsolationMode
		want string
	}{
		{IsolationNamespace, "namespace"},
		{IsolationProcess, "process"},
		{IsolationMode(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("IsolationMode(%d).String() = %q, want %q", int(tt.mode), got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/suryakoritala/Cyntr && go test ./tenant/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement tenant manager**

Create `tenant/tenant.go`:
```go
package tenant

import (
	"fmt"
	"sort"
	"sync"

	"github.com/cyntr-dev/cyntr/kernel/config"
	"github.com/cyntr-dev/cyntr/kernel/resource"
)

// IsolationMode defines how a tenant's agents are isolated.
type IsolationMode int

const (
	IsolationNamespace IsolationMode = iota // goroutines in shared process
	IsolationProcess                        // separate OS processes
)

func (m IsolationMode) String() string {
	switch m {
	case IsolationNamespace:
		return "namespace"
	case IsolationProcess:
		return "process"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}

// Tenant represents a single tenant in the system.
type Tenant struct {
	Name      string
	Isolation IsolationMode
	Policy    string // policy file/name reference
}

// Manager handles tenant lifecycle — CRUD, config loading, resource limits.
type Manager struct {
	mu        sync.RWMutex
	tenants   map[string]*Tenant
	resources *resource.Manager
}

// NewManager creates a TenantManager from config, applying resource limits.
func NewManager(cfg config.CyntrConfig, rm *resource.Manager) (*Manager, error) {
	tm := &Manager{
		tenants:   make(map[string]*Tenant),
		resources: rm,
	}

	for name, tc := range cfg.Tenants {
		mode, err := parseIsolation(tc.Isolation)
		if err != nil {
			return nil, fmt.Errorf("tenant %q: %w", name, err)
		}
		tm.tenants[name] = &Tenant{
			Name:      name,
			Isolation: mode,
			Policy:    tc.Policy,
		}
	}

	return tm, nil
}

// Get returns a tenant by name.
func (m *Manager) Get(name string) (Tenant, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, ok := m.tenants[name]
	if !ok {
		return Tenant{}, false
	}
	return *t, true
}

// List returns all tenant names sorted alphabetically.
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.tenants))
	for name := range m.tenants {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Create adds a new tenant. Returns error if it already exists.
func (m *Manager) Create(name string, isolation IsolationMode, policy string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[name]; exists {
		return fmt.Errorf("tenant %q already exists", name)
	}

	m.tenants[name] = &Tenant{
		Name:      name,
		Isolation: isolation,
		Policy:    policy,
	}
	return nil
}

// Delete removes a tenant. Returns error if not found.
func (m *Manager) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[name]; !exists {
		return fmt.Errorf("tenant %q not found", name)
	}

	delete(m.tenants, name)
	return nil
}

func parseIsolation(s string) (IsolationMode, error) {
	switch s {
	case "namespace", "":
		return IsolationNamespace, nil
	case "process":
		return IsolationProcess, nil
	default:
		return IsolationNamespace, fmt.Errorf("invalid isolation mode %q", s)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./tenant/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add tenant/tenant.go tenant/tenant_test.go
git commit -m "feat(tenant): implement TenantManager with CRUD and config loading"
```

---

## Chunk 2: Auth Types + RBAC

### Task 2: Define Auth Types

**Files:**
- Create: `auth/types.go`
- Create: `auth/types_test.go`

- [ ] **Step 1: Write failing tests**

Create `auth/types_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./auth/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement auth types**

Create `auth/types.go`:
```go
package auth

// PrincipalType distinguishes users from agents.
type PrincipalType int

const (
	PrincipalUser  PrincipalType = iota
	PrincipalAgent
)

// Principal represents an authenticated identity in the system.
type Principal struct {
	Type        PrincipalType
	ID          string   // user email or agent name
	Tenant      string   // which tenant this principal belongs to
	Roles       []string // role names assigned to this principal
	Permissions []Permission // resolved permissions from roles
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/suryakoritala/Cyntr && go test ./auth/ -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add auth/types.go auth/types_test.go
git commit -m "feat(auth): define Principal, Role, Permission types"
```

---

### Task 3: Implement RBAC Permission Checking

**Files:**
- Create: `auth/rbac.go`
- Create: `auth/rbac_test.go`

- [ ] **Step 1: Write failing tests**

Create `auth/rbac_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./auth/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement RBAC**

Create `auth/rbac.go`:
```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./auth/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add auth/rbac.go auth/rbac_test.go
git commit -m "feat(auth): implement RBAC with built-in roles and permission checking"
```

---

## Chunk 3: Session Management

### Task 4: Implement JWT Sessions and API Keys

**Files:**
- Create: `auth/session.go`
- Create: `auth/session_test.go`

- [ ] **Step 1: Write failing tests**

Create `auth/session_test.go`:
```go
package auth

import (
	"testing"
	"time"
)

func TestSessionManagerCreateAndValidateJWT(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")

	principal := Principal{
		Type:   PrincipalUser,
		ID:     "jane@corp.com",
		Tenant: "finance",
		Roles:  []string{"admin"},
	}

	token, err := sm.CreateToken(principal, 1*time.Hour)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	resolved, err := sm.ValidateToken(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if resolved.ID != "jane@corp.com" {
		t.Fatalf("expected jane, got %q", resolved.ID)
	}
	if resolved.Tenant != "finance" {
		t.Fatalf("expected finance, got %q", resolved.Tenant)
	}
	if len(resolved.Roles) != 1 || resolved.Roles[0] != "admin" {
		t.Fatalf("expected [admin], got %v", resolved.Roles)
	}
}

func TestSessionManagerExpiredToken(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")

	principal := Principal{Type: PrincipalUser, ID: "jane@corp.com", Tenant: "finance"}

	token, _ := sm.CreateToken(principal, -1*time.Hour) // already expired

	_, err := sm.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestSessionManagerInvalidToken(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")

	_, err := sm.ValidateToken("garbage.token.here")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestSessionManagerWrongSecret(t *testing.T) {
	sm1 := NewSessionManager("secret-one-minimum-32-bytes-long!")
	sm2 := NewSessionManager("secret-two-minimum-32-bytes-long!")

	principal := Principal{Type: PrincipalUser, ID: "jane@corp.com", Tenant: "finance"}
	token, _ := sm1.CreateToken(principal, 1*time.Hour)

	_, err := sm2.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestAPIKeyCreateAndValidate(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")

	principal := Principal{
		Type:   PrincipalUser,
		ID:     "ci-bot",
		Tenant: "devops",
		Roles:  []string{"admin"},
	}

	key, err := sm.CreateAPIKey("ci-deploy", principal)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	if key == "" {
		t.Fatal("expected non-empty key")
	}

	resolved, err := sm.ValidateAPIKey(key)
	if err != nil {
		t.Fatalf("validate key: %v", err)
	}
	if resolved.ID != "ci-bot" {
		t.Fatalf("expected ci-bot, got %q", resolved.ID)
	}
}

func TestAPIKeyInvalid(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")

	_, err := sm.ValidateAPIKey("invalid-key")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./auth/ -run TestSession -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement session manager**

Create `auth/session.go`:
```go
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// SessionManager handles JWT token creation/validation and API keys.
type SessionManager struct {
	secret []byte
	mu     sync.RWMutex
	apiKeys map[string]Principal // key hash -> principal
}

// NewSessionManager creates a session manager with the given signing secret.
func NewSessionManager(secret string) *SessionManager {
	return &SessionManager{
		secret:  []byte(secret),
		apiKeys: make(map[string]Principal),
	}
}

// jwtHeader is the fixed JWT header for HS256.
var jwtHeader = base64url([]byte(`{"alg":"HS256","typ":"JWT"}`))

type jwtClaims struct {
	Sub    string   `json:"sub"`
	Tenant string   `json:"tenant"`
	Type   int      `json:"type"`
	Roles  []string `json:"roles"`
	Exp    int64    `json:"exp"`
	Iat    int64    `json:"iat"`
}

// CreateToken creates a signed JWT for the given principal.
func (sm *SessionManager) CreateToken(p Principal, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwtClaims{
		Sub:    p.ID,
		Tenant: p.Tenant,
		Type:   int(p.Type),
		Roles:  p.Roles,
		Iat:    now.Unix(),
		Exp:    now.Add(ttl).Unix(),
	}

	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}

	payload := base64url(claimsJSON)
	signingInput := jwtHeader + "." + payload

	mac := hmac.New(sha256.New, sm.secret)
	mac.Write([]byte(signingInput))
	signature := base64url(mac.Sum(nil))

	return signingInput + "." + signature, nil
}

// ValidateToken validates a JWT and returns the principal.
func (sm *SessionManager) ValidateToken(token string) (Principal, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Principal{}, fmt.Errorf("invalid token format")
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, sm.secret)
	mac.Write([]byte(signingInput))
	expectedSig := base64url(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return Principal{}, fmt.Errorf("invalid token signature")
	}

	claimsJSON, err := base64urlDecode(parts[1])
	if err != nil {
		return Principal{}, fmt.Errorf("decode claims: %w", err)
	}

	var claims jwtClaims
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return Principal{}, fmt.Errorf("parse claims: %w", err)
	}

	if time.Now().Unix() > claims.Exp {
		return Principal{}, fmt.Errorf("token expired")
	}

	return Principal{
		Type:   PrincipalType(claims.Type),
		ID:     claims.Sub,
		Tenant: claims.Tenant,
		Roles:  claims.Roles,
	}, nil
}

// CreateAPIKey generates a random API key and associates it with a principal.
func (sm *SessionManager) CreateAPIKey(name string, p Principal) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}

	key := "cyntr_" + hex.EncodeToString(buf)

	hash := sha256.Sum256([]byte(key))
	keyHash := hex.EncodeToString(hash[:])

	sm.mu.Lock()
	sm.apiKeys[keyHash] = p
	sm.mu.Unlock()

	return key, nil
}

// ValidateAPIKey checks an API key and returns the associated principal.
func (sm *SessionManager) ValidateAPIKey(key string) (Principal, error) {
	hash := sha256.Sum256([]byte(key))
	keyHash := hex.EncodeToString(hash[:])

	sm.mu.RLock()
	p, ok := sm.apiKeys[keyHash]
	sm.mu.RUnlock()

	if !ok {
		return Principal{}, fmt.Errorf("invalid API key")
	}
	return p, nil
}

func base64url(data []byte) string {
	return strings.TrimRight(base64.URLEncoding.EncodeToString(data), "=")
}

func base64urlDecode(s string) ([]byte, error) {
	// Add padding back
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./auth/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add auth/session.go auth/session_test.go
git commit -m "feat(auth): implement JWT session tokens and API key management"
```

---

## Chunk 4: Identity Mapper + Auth Module

### Task 5: Implement Identity Mapper

**Files:**
- Create: `auth/identity.go`
- Create: `auth/identity_test.go`

- [ ] **Step 1: Write failing tests**

Create `auth/identity_test.go`:
```go
package auth

import "testing"

func TestIdentityMapperResolveJWT(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	rbac := NewRBAC()
	mapper := NewIdentityMapper(sm, rbac)

	principal := Principal{Type: PrincipalUser, ID: "jane@corp.com", Tenant: "finance", Roles: []string{"admin"}}
	token, _ := sm.CreateToken(principal, 3600_000_000_000) // 1 hour in ns

	resolved, err := mapper.Resolve("bearer", token)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.ID != "jane@corp.com" {
		t.Fatalf("expected jane, got %q", resolved.ID)
	}
	// Permissions should be populated from RBAC
	if len(resolved.Permissions) == 0 {
		t.Fatal("expected permissions from admin role")
	}
}

func TestIdentityMapperResolveAPIKey(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	rbac := NewRBAC()
	mapper := NewIdentityMapper(sm, rbac)

	principal := Principal{Type: PrincipalUser, ID: "ci-bot", Tenant: "devops", Roles: []string{"admin"}}
	key, _ := sm.CreateAPIKey("ci-deploy", principal)

	resolved, err := mapper.Resolve("apikey", key)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.ID != "ci-bot" {
		t.Fatalf("expected ci-bot, got %q", resolved.ID)
	}
}

func TestIdentityMapperResolveUnknownScheme(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	rbac := NewRBAC()
	mapper := NewIdentityMapper(sm, rbac)

	_, err := mapper.Resolve("oauth2", "token")
	if err == nil {
		t.Fatal("expected error for unknown scheme")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./auth/ -run TestIdentity -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement identity mapper**

Create `auth/identity.go`:
```go
package auth

import "fmt"

// IdentityMapper resolves credentials to enriched principals.
type IdentityMapper struct {
	sessions *SessionManager
	rbac     *RBAC
}

// NewIdentityMapper creates an identity mapper.
func NewIdentityMapper(sessions *SessionManager, rbac *RBAC) *IdentityMapper {
	return &IdentityMapper{sessions: sessions, rbac: rbac}
}

// Resolve takes an auth scheme and credential, returns an enriched principal.
// Supported schemes: "bearer" (JWT), "apikey" (API key).
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

	if err != nil {
		return Principal{}, fmt.Errorf("auth failed: %w", err)
	}

	// Enrich with resolved permissions from RBAC
	p.Permissions = m.resolvePermissions(p)

	return p, nil
}

func (m *IdentityMapper) resolvePermissions(p Principal) []Permission {
	seen := make(map[Permission]bool)
	var perms []Permission

	for _, roleName := range p.Roles {
		for _, perm := range m.rbac.RolePermissions(roleName) {
			if !seen[perm] {
				seen[perm] = true
				perms = append(perms, perm)
			}
		}
	}

	return perms
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./auth/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add auth/identity.go auth/identity_test.go
git commit -m "feat(auth): implement IdentityMapper resolving credentials to enriched principals"
```

---

### Task 6: Implement Auth Kernel Module

**Files:**
- Create: `auth/module.go`
- Create: `auth/module_test.go`

- [ ] **Step 1: Write failing tests**

Create `auth/module_test.go`:
```go
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

// ResolveRequest is the IPC payload for auth.resolve.
type testResolveReq struct {
	Scheme     string
	Credential string
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

	// Create a token to resolve
	principal := Principal{Type: PrincipalUser, ID: "jane@corp.com", Tenant: "finance", Roles: []string{"admin"}}
	token, _ := sm.CreateToken(principal, 1*time.Hour)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "auth", Topic: "auth.resolve",
		Payload: ResolveRequest{Scheme: "bearer", Credential: token},
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	resolved, ok := resp.Payload.(Principal)
	if !ok {
		t.Fatalf("expected Principal, got %T", resp.Payload)
	}
	if resolved.ID != "jane@corp.com" {
		t.Fatalf("expected jane, got %q", resolved.ID)
	}
	if len(resolved.Permissions) == 0 {
		t.Fatal("expected permissions to be populated")
	}
}

func TestAuthModuleHealthy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	rbac := NewRBAC()
	mod := NewModule(sm, rbac)
	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)

	health := mod.Health(ctx)
	if !health.Healthy {
		t.Fatalf("expected healthy: %s", health.Message)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./auth/ -run TestAuthModule -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement auth module**

Create `auth/module.go`:
```go
package auth

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// ResolveRequest is the IPC payload for auth.resolve requests.
type ResolveRequest struct {
	Scheme     string
	Credential string
}

// Module is the Auth kernel module.
type Module struct {
	mapper *IdentityMapper
	bus    *ipc.Bus
}

// NewModule creates a new Auth module.
func NewModule(sm *SessionManager, rbac *RBAC) *Module {
	return &Module{
		mapper: NewIdentityMapper(sm, rbac),
	}
}

func (m *Module) Name() string           { return "auth" }
func (m *Module) Dependencies() []string { return nil }

func (m *Module) Init(ctx context.Context, svc *kernel.Services) error {
	m.bus = svc.Bus
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	m.bus.Handle("auth", "auth.resolve", m.handleResolve)
	return nil
}

func (m *Module) Stop(ctx context.Context) error { return nil }

func (m *Module) Health(ctx context.Context) kernel.HealthStatus {
	return kernel.HealthStatus{Healthy: true, Message: "auth module running"}
}

func (m *Module) handleResolve(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(ResolveRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected ResolveRequest, got %T", msg.Payload)
	}

	principal, err := m.mapper.Resolve(req.Scheme, req.Credential)
	if err != nil {
		return ipc.Message{}, err
	}

	return ipc.Message{
		Type:    ipc.MessageTypeResponse,
		Payload: principal,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./auth/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add auth/module.go auth/module_test.go
git commit -m "feat(auth): implement Auth kernel module with IPC auth.resolve handler"
```

---

## Chunk 5: Integration Test + Final Verification

### Task 7: Tenant + Auth Integration Test

**Files:**
- Modify: `tests/integration/policy_audit_test.go` (add tenant+auth test)

- [ ] **Step 1: Write integration test**

Add to `tests/integration/policy_audit_test.go` (or create a new file `tests/integration/tenant_auth_test.go`):

Create `tests/integration/tenant_auth_test.go`:
```go
package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/auth"
	"github.com/cyntr-dev/cyntr/tenant"
	"github.com/cyntr-dev/cyntr/kernel/config"
	"github.com/cyntr-dev/cyntr/kernel/resource"
)

func TestTenantAuthPolicyFlow(t *testing.T) {
	dir := t.TempDir()

	// Policy: finance denies shell, marketing allows everything
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

	// Set up kernel
	k := kernel.New()
	if err := k.LoadConfig(cfgPath); err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Create tenant manager from config
	cfgStore := k.Config()
	rm := k.ResourceManager()
	tm, err := tenant.NewManager(cfgStore.Get(), rm)
	if err != nil {
		t.Fatalf("tenant manager: %v", err)
	}

	// Verify tenants loaded
	tenants := tm.List()
	if len(tenants) != 2 {
		t.Fatalf("expected 2 tenants, got %d", len(tenants))
	}

	fin, ok := tm.Get("finance")
	if !ok {
		t.Fatal("expected finance tenant")
	}
	if fin.Isolation != tenant.IsolationProcess {
		t.Fatalf("expected process isolation for finance, got %s", fin.Isolation)
	}

	// Create auth components
	sm := auth.NewSessionManager("test-secret-key-minimum-32-bytes!")
	rbac := auth.NewRBAC()
	authMod := auth.NewModule(sm, rbac)

	// Register modules
	policyEngine := policy.NewEngine(policyPath)
	if err := k.Register(policyEngine); err != nil {
		t.Fatalf("register policy: %v", err)
	}
	if err := k.Register(authMod); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer k.Stop(ctx)

	bus := k.Bus()

	// Create a JWT for a finance admin
	financePrincipal := auth.Principal{
		Type: auth.PrincipalUser, ID: "jane@corp.com",
		Tenant: "finance", Roles: []string{"admin"},
	}
	token, _ := sm.CreateToken(financePrincipal, 1*time.Hour)

	// Resolve the token via IPC
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	authResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "auth", Topic: "auth.resolve",
		Payload: auth.ResolveRequest{Scheme: "bearer", Credential: token},
	})
	if err != nil {
		t.Fatalf("auth resolve: %v", err)
	}

	resolved := authResp.Payload.(auth.Principal)
	if resolved.ID != "jane@corp.com" {
		t.Fatalf("expected jane, got %q", resolved.ID)
	}

	// Check if finance admin has manage_tenants permission
	if !rbac.HasPermission(resolved, auth.PermManageTenants) {
		t.Fatal("expected admin to have manage_tenants")
	}

	// Check policy: finance + shell_exec should be denied
	policyResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "policy", Topic: "policy.check",
		Payload: policy.CheckRequest{
			Tenant: resolved.Tenant, Action: "tool_call",
			Tool: "shell_exec", Agent: "assistant", User: resolved.ID,
		},
	})
	if err != nil {
		t.Fatalf("policy check: %v", err)
	}

	checkResp := policyResp.Payload.(policy.CheckResponse)
	if checkResp.Decision != policy.Deny {
		t.Fatalf("expected deny for finance+shell, got %s", checkResp.Decision)
	}
}
```

- [ ] **Step 2: Add Config() and ResourceManager() accessors to kernel**

Add to `kernel/kernel.go`:
```go
// Config returns the config store for external use.
func (k *Kernel) Config() *config.Store {
	return k.services.Config
}

// ResourceManager returns the resource manager for external use.
func (k *Kernel) ResourceManager() *resource.Manager {
	return k.services.Resources
}
```

- [ ] **Step 3: Run integration test**

Run: `cd /Users/suryakoritala/Cyntr && go test ./tests/integration/ -v -count=1 -race`
Expected: PASS

- [ ] **Step 4: Run full test suite**

Run: `cd /Users/suryakoritala/Cyntr && go test ./... -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Run go vet**

Run: `cd /Users/suryakoritala/Cyntr && go vet ./...`
Expected: No issues

- [ ] **Step 6: Commit**

```bash
git add tests/integration/tenant_auth_test.go kernel/kernel.go
git commit -m "feat: add integration test — tenant isolation + auth + policy flow via IPC"
```

---

### Task 8: Final Verification

- [ ] **Step 1: Run complete test suite**

Run: `cd /Users/suryakoritala/Cyntr && go test ./... -count=1 -race`
Expected: All PASS

- [ ] **Step 2: Run go vet**

Run: `cd /Users/suryakoritala/Cyntr && go vet ./...`

- [ ] **Step 3: Build binary**

Run: `cd /Users/suryakoritala/Cyntr && go build -o cyntr ./cmd/cyntr && ./cyntr version`
Expected: `cyntr v0.1.0`

- [ ] **Step 4: Verify new file structure**

Run: `cd /Users/suryakoritala/Cyntr && find . -name '*.go' -path '*/tenant/*' -o -name '*.go' -path '*/auth/*' | sort`
Expected:
```
./auth/identity.go
./auth/identity_test.go
./auth/module.go
./auth/module_test.go
./auth/rbac.go
./auth/rbac_test.go
./auth/session.go
./auth/session_test.go
./auth/types.go
./auth/types_test.go
./tenant/tenant.go
./tenant/tenant_test.go
```
