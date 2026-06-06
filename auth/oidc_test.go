package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockIDToken builds an UNSIGNED token (stub signature) for tests that do not
// exercise the signature-verification path.
func mockIDToken(email, name string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims := fmt.Sprintf(`{"sub":"user123","email":"%s","name":"%s"}`, email, name)
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	signature := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	return header + "." + payload + "." + signature
}

const testKID = "test-key-1"

// signedIDToken builds a real RS256-signed id_token for the given claims so
// tests exercise actual signature verification rather than a stub.
func signedIDToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"` + testKID + `"}`))
	cj, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(cj)
	signingInput := header + "." + payload
	hashed := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
	if err != nil {
		t.Fatalf("sign id_token: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// newJWKSServer serves a JWKS exposing key's public half under testKID.
func newJWKSServer(t *testing.T, key *rsa.PrivateKey) *httptest.Server {
	t.Helper()
	eBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(eBuf, uint32(key.PublicKey.E))
	eBuf = eBuf[1:] // 65537 fits in 3 bytes; trim the leading zero
	jwks := map[string]any{"keys": []map[string]string{{
		"kid": testKID, "kty": "RSA", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(eBuf),
	}}}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jwks)
	}))
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
	t.Setenv("CYNTR_SSRF_ALLOW_PRIVATE", "1")
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwksServer := newJWKSServer(t, key)
	defer jwksServer.Close()
	idToken := signedIDToken(t, key, map[string]any{
		"sub": "user123", "email": "jane@corp.com", "name": "Jane Doe",
		"aud": "app", "exp": time.Now().Add(time.Hour).Unix(),
	})

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
	p.SetJWKSURI(jwksServer.URL)

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

func TestOIDCProviderDiscoverFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	p := NewOIDCProvider(OIDCConfig{Issuer: server.URL}, sm)
	err := p.Discover(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOIDCProviderDiscoverBadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not json")
	}))
	defer server.Close()

	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	p := NewOIDCProvider(OIDCConfig{Issuer: server.URL}, sm)
	err := p.Discover(context.Background())
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestOIDCProviderAuthURLDefaultScopes(t *testing.T) {
	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	p := NewOIDCProvider(OIDCConfig{ClientID: "app"}, sm) // no scopes set
	p.SetEndpoints("https://idp/auth", "", "")

	url := p.AuthURL("state")
	if !strings.Contains(url, "openid") {
		t.Fatal("expected default openid scope")
	}
	if !strings.Contains(url, "email") {
		t.Fatal("expected default email scope")
	}
}

func TestOIDCProviderExchangeCodeMissingEmail(t *testing.T) {
	t.Setenv("CYNTR_SSRF_ALLOW_PRIVATE", "1")
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwksServer := newJWKSServer(t, key)
	defer jwksServer.Close()
	// Properly signed token with valid aud/exp but no email field.
	idToken := signedIDToken(t, key, map[string]any{
		"sub": "user1", "aud": "app", "exp": time.Now().Add(time.Hour).Unix(),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"id_token": idToken})
	}))
	defer server.Close()

	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	p := NewOIDCProvider(OIDCConfig{ClientID: "app", ClientSecret: "secret"}, sm)
	p.SetEndpoints("", server.URL, "")
	p.SetJWKSURI(jwksServer.URL)

	// Should succeed but with empty email
	token, err := p.ExchangeCode(context.Background(), "code", "tenant", nil)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	principal, _ := sm.ValidateToken(token)
	if principal.ID != "" {
		t.Fatalf("expected empty ID for missing email, got %q", principal.ID)
	}
}

// A forged (wrong-signature) id_token must be rejected, and a JWKS fetch
// failure must fail closed rather than skip verification.
func TestOIDCProviderRejectsForgedAndUnverifiableTokens(t *testing.T) {
	t.Setenv("CYNTR_SSRF_ALLOW_PRIVATE", "1")
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	attacker, _ := rsa.GenerateKey(rand.Reader, 2048)
	jwksServer := newJWKSServer(t, key)
	defer jwksServer.Close()

	// Token signed by an attacker key not in the JWKS.
	forged := signedIDToken(t, attacker, map[string]any{
		"sub": "x", "email": "admin@victim.com", "aud": "app",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"id_token": forged})
	}))
	defer tokenServer.Close()

	sm := NewSessionManager("test-secret-key-minimum-32-bytes!")
	p := NewOIDCProvider(OIDCConfig{ClientID: "app"}, sm)
	p.SetEndpoints("", tokenServer.URL, "")
	p.SetJWKSURI(jwksServer.URL)
	if _, err := p.ExchangeCode(context.Background(), "code", "t", nil); err == nil {
		t.Fatal("forged token must be rejected")
	}

	// JWKS unreachable -> must fail closed (no jwks_uri configured).
	p2 := NewOIDCProvider(OIDCConfig{ClientID: "app"}, sm)
	p2.SetEndpoints("", tokenServer.URL, "")
	if _, err := p2.ExchangeCode(context.Background(), "code", "t", nil); err == nil {
		t.Fatal("unverifiable token (no JWKS) must fail closed")
	}
}

func TestBase64URLDecode(t *testing.T) {
	// Standard base64url without padding
	decoded, err := base64URLDecode("SGVsbG8")
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "Hello" {
		t.Fatalf("expected Hello, got %q", decoded)
	}
}

func TestBase64URLDecodeWithPadding(t *testing.T) {
	decoded, err := base64URLDecode("SGVsbG8gV29ybGQ")
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "Hello World" {
		t.Fatalf("expected 'Hello World', got %q", decoded)
	}
}

func TestVerifyIDTokenBadFormat(t *testing.T) {
	p := &OIDCProvider{}
	err := p.verifyIDTokenSignature("not-a-jwt")
	if err == nil {
		t.Fatal("expected error for invalid JWT format")
	}
}

func TestVerifyIDTokenUnsupportedAlg(t *testing.T) {
	// Create a JWT with HS256 header
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"test"}`))
	token := header + "." + payload + ".fakesig"

	p := &OIDCProvider{}
	err := p.verifyIDTokenSignature(token)
	if err == nil {
		t.Fatal("expected error for unsupported algorithm")
	}
}
