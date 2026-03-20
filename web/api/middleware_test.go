package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
