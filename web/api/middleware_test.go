package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyntr-dev/cyntr/auth"
)

func TestAuthMiddlewareDisabled(t *testing.T) {
	am := NewAuthMiddleware(AuthConfig{Enabled: false})
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/tenants", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAuthMiddlewareBlocksWithout(t *testing.T) {
	am := NewAuthMiddleware(AuthConfig{Enabled: true})
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/tenants", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddlewareAllowsAPIKey(t *testing.T) {
	am := NewAuthMiddleware(AuthConfig{Enabled: true, APIKeys: map[string]string{"test-key-123": "test"}})
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer test-key-123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAuthMiddlewareSkipsHealth(t *testing.T) {
	am := NewAuthMiddleware(AuthConfig{Enabled: true})
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/system/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestAuthMiddlewareInvalidKey(t *testing.T) {
	am := NewAuthMiddleware(AuthConfig{Enabled: true, APIKeys: map[string]string{"real-key": "valid"}})
	handler := am.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// --- Scope tests ---

func TestHasScopeAdmin(t *testing.T) {
	if !hasScope([]string{auth.ScopeAdmin}, auth.ScopeRead) {
		t.Fatal("admin should have read access")
	}
}

func TestHasScopeReadOnly(t *testing.T) {
	if hasScope([]string{auth.ScopeRead}, auth.ScopeAdmin) {
		t.Fatal("read-only should not have admin access")
	}
}

func TestHasScopeEmpty(t *testing.T) {
	if !hasScope([]string{}, auth.ScopeAdmin) {
		t.Fatal("empty scopes should allow all (backward compat)")
	}
}

func TestHasScopeMatch(t *testing.T) {
	if !hasScope([]string{auth.ScopeAgent}, auth.ScopeAgent) {
		t.Fatal("matching scope should be allowed")
	}
}

func TestAuthMiddlewareBlocksUnauthenticated(t *testing.T) {
	mw := NewAuthMiddleware(AuthConfig{
		Enabled: true,
		APIKeys: map[string]string{"test-key": "default"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/tenants", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 401 {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthMiddlewareAllowsValidKey(t *testing.T) {
	mw := NewAuthMiddleware(AuthConfig{
		Enabled: true,
		APIKeys: map[string]string{"test-key": "default"},
	})

	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAuthMiddlewareSkipsHealthEndpoint(t *testing.T) {
	mw := NewAuthMiddleware(AuthConfig{Enabled: true, APIKeys: map[string]string{}})
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/api/v1/system/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("health should bypass auth, got %d", rec.Code)
	}
}
