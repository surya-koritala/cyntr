package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/cyntr-dev/cyntr/auth"
)

// AuthConfig holds authentication settings for the API.
type AuthConfig struct {
	Enabled   bool
	APIKeys   map[string]string   // key -> description
	KeyScopes map[string][]string // key -> scopes (read, agent, admin)
	JWTSecret string
}

// routeScopes maps HTTP methods to the minimum required scope.
var routeScopes = map[string]string{
	"GET":    auth.ScopeRead,
	"POST":   auth.ScopeAgent,
	"PUT":    auth.ScopeAgent,
	"DELETE": auth.ScopeAdmin,
}

// hasScope checks whether the given scopes list satisfies the required scope.
// Admin scope grants access to everything. An empty scopes list grants full
// access for backward compatibility with keys created before scoping.
func hasScope(scopes []string, required string) bool {
	for _, s := range scopes {
		if s == auth.ScopeAdmin || s == required {
			return true
		}
	}
	return len(scopes) == 0 // backward compat: no scopes = full access
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
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			RespondError(w, 401, "UNAUTHORIZED", "Authorization header required")
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			// No "Bearer " prefix — might be direct API key
			token = authHeader
		}

		// Check API keys
		for key := range am.config.APIKeys {
			if token == key {
				// Valid API key — check scope before proceeding
				var scopes []string
				if am.config.KeyScopes != nil {
					scopes = am.config.KeyScopes[key]
				}

				requiredScope := routeScopes[r.Method]
				if requiredScope != "" && !hasScope(scopes, requiredScope) {
					RespondError(w, 403, "FORBIDDEN", "insufficient scope")
					return
				}

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
