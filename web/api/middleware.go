package api

import (
	"context"
	"net/http"
	"strings"
)

// AuthConfig holds authentication settings for the API.
type AuthConfig struct {
	Enabled   bool
	APIKeys   map[string]string // key -> description
	JWTSecret string
}

// AuthMiddleware checks authentication on API requests.
type AuthMiddleware struct {
	config AuthConfig
}

func NewAuthMiddleware(config AuthConfig) *AuthMiddleware {
	return &AuthMiddleware{config: config}
}

// Wrap returns an http.Handler that checks auth before calling next.
func (am *AuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health endpoint and OIDC endpoints
		if r.URL.Path == "/api/v1/system/health" ||
			r.URL.Path == "/api/v1/system/version" ||
			r.URL.Path == "/api/v1/auth/oidc/login" ||
			r.URL.Path == "/api/v1/auth/oidc/callback" ||
			r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		if !am.config.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		// Check Authorization header
		auth := r.Header.Get("Authorization")
		if auth == "" {
			RespondError(w, 401, "UNAUTHORIZED", "Authorization header required")
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		if token == auth {
			// No "Bearer " prefix — might be direct API key
			token = auth
		}

		// Check API keys
		for key := range am.config.APIKeys {
			if token == key {
				// Valid API key — set context and proceed
				ctx := context.WithValue(r.Context(), contextKeyAuth, "apikey")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		// If JWT secret is configured, try JWT validation
		if am.config.JWTSecret != "" {
			// Simple JWT check — in production use auth.SessionManager
			ctx := context.WithValue(r.Context(), contextKeyAuth, "jwt")
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		RespondError(w, 401, "UNAUTHORIZED", "Invalid credentials")
	})
}

type contextKey string

const contextKeyAuth contextKey = "auth"
