package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mockIDToken(email, name string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims := fmt.Sprintf(`{"sub":"user123","email":"%s","name":"%s"}`, email, name)
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	signature := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	return header + "." + payload + "." + signature
}

func TestOIDCProviderAuthURL(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	p := NewOIDCProvider(OIDCConfig{
		ClientID: "my-app", RedirectURL: "http://localhost:7700/callback",
		Scopes: []string{"openid", "email"},
	}, sm)
	p.SetEndpoints("https://idp.example.com/authorize", "", "")

	url := p.AuthURL("state123")
	if !strings.Contains(url, "client_id=my-app") {
		t.Fatalf("missing client_id: %s", url)
	}
	if !strings.Contains(url, "state=state123") {
		t.Fatalf("missing state: %s", url)
	}
	if !strings.Contains(url, "response_type=code") {
		t.Fatalf("missing response_type: %s", url)
	}
	if !strings.Contains(url, "openid") {
		t.Fatalf("missing scope: %s", url)
	}
}

func TestOIDCProviderExchangeCode(t *testing.T) {
	idToken := mockIDToken("jane@corp.com", "Jane Doe")

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatal("expected POST")
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatal("wrong content type")
		}
		json.NewEncoder(w).Encode(map[string]string{
			"id_token": idToken, "access_token": "at_test",
		})
	}))
	defer tokenServer.Close()

	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	p := NewOIDCProvider(OIDCConfig{
		ClientID: "app", ClientSecret: "secret",
		RedirectURL: "http://localhost/callback",
	}, sm)
	p.SetEndpoints("", tokenServer.URL, "")

	token, err := p.ExchangeCode(context.Background(), "auth-code-123", "finance", []string{"admin"})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if token == "" {
		t.Fatal("expected token")
	}

	// Validate the Cyntr JWT
	principal, err := sm.ValidateToken(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if principal.ID != "jane@corp.com" {
		t.Fatalf("expected jane, got %q", principal.ID)
	}
	if principal.Tenant != "finance" {
		t.Fatalf("expected finance, got %q", principal.Tenant)
	}
}

func TestOIDCProviderDiscover(t *testing.T) {
	discovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": "https://idp/auth",
			"token_endpoint":         "https://idp/token",
			"userinfo_endpoint":      "https://idp/userinfo",
		})
	}))
	defer discovery.Close()

	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	p := NewOIDCProvider(OIDCConfig{Issuer: discovery.URL}, sm)

	if err := p.Discover(context.Background()); err != nil {
		t.Fatalf("discover: %v", err)
	}

	url := p.AuthURL("state")
	if !strings.Contains(url, "https://idp/auth") {
		t.Fatalf("expected discovered auth URL, got %s", url)
	}
}

func TestOIDCProviderExchangeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":"invalid_grant"}`)
	}))
	defer server.Close()

	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	p := NewOIDCProvider(OIDCConfig{ClientID: "app", ClientSecret: "secret"}, sm)
	p.SetEndpoints("", server.URL, "")

	_, err := p.ExchangeCode(context.Background(), "bad-code", "t", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseIDTokenClaims(t *testing.T) {
	token := mockIDToken("test@example.com", "Test User")
	claims, err := parseIDTokenClaims(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Email != "test@example.com" {
		t.Fatalf("got %q", claims.Email)
	}
	if claims.Name != "Test User" {
		t.Fatalf("got %q", claims.Name)
	}
}

func TestParseIDTokenInvalid(t *testing.T) {
	_, err := parseIDTokenClaims("not.a.valid.jwt")
	// This has 4 parts, not 3
	if err == nil {
		t.Fatal("expected error")
	}
}
