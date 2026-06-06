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
	sm     *auth.SessionManager // verifies JWT bearer tokens (HS256)
}

func NewAuthMiddleware(config AuthConfig) *AuthMiddleware {
	am := &AuthMiddleware{config: config}
	if config.JWTSecret != "" {
		am.sm = auth.NewSessionManager(config.JWTSecret)
	}
	return am
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
		var token string
		if authHeader != "" {
			token = strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader {
				// No "Bearer " prefix — might be direct API key
				token = authHeader
			}
		}

		// Support API key via query param (for EventSource/SSE)
		if token == "" {
			if qkey := r.URL.Query().Get("key"); qkey != "" {
				token = qkey
			}
		}

		if token == "" {
			RespondError(w, 401, "UNAUTHORIZED", "Authorization header required")
			return
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

		// If a JWT secret is configured, verify the bearer token's signature,
		// expiry, and scope. NEVER grant access merely because a secret is set.
		if am.sm != nil {
			p, err := am.sm.ValidateToken(token)
			if err != nil {
				RespondError(w, 401, "UNAUTHORIZED", "Invalid credentials")
				return
			}
			requiredScope := routeScopes[r.Method]
			if requiredScope != "" && !hasScope(jwtScopes(p), requiredScope) {
				RespondError(w, 403, "FORBIDDEN", "insufficient scope")
				return
			}
			ctx := context.WithValue(r.Context(), contextKeyAuth, "jwt")
			ctx = context.WithValue(ctx, contextKeyPrincipal, p)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		RespondError(w, 401, "UNAUTHORIZED", "Invalid credentials")
	})
}

// jwtScopes derives API scopes from a JWT principal's roles. The "admin" role
// grants admin scope; "agent"/"read" map to their like-named scopes. A
// principal with no recognized roles gets read-only.
func jwtScopes(p auth.Principal) []string {
	scopes := make([]string, 0, len(p.Roles))
	for _, role := range p.Roles {
		switch role {
		case auth.ScopeAdmin, auth.ScopeAgent, auth.ScopeRead:
			scopes = append(scopes, role)
		}
	}
	if len(scopes) == 0 {
		scopes = []string{auth.ScopeRead}
	}
	return scopes
}

type contextKey string

const (
	contextKeyAuth      contextKey = "auth"
	contextKeyPrincipal contextKey = "principal"
)

// authPrincipal returns the authenticated JWT principal stashed by the
// middleware, or ok=false when the request was authenticated another way
// (API key) or auth is disabled.
func authPrincipal(r *http.Request) (auth.Principal, bool) {
	p, ok := r.Context().Value(contextKeyPrincipal).(auth.Principal)
	return p, ok
}
