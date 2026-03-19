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
