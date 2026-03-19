package api

import (
	"net/http"
)

func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	// In production, this would redirect to the OIDC auth URL
	Respond(w, 200, map[string]string{
		"message": "OIDC login endpoint. Configure OIDC provider to enable.",
		"status":  "not_configured",
	})
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" {
		RespondError(w, 400, "MISSING_CODE", "authorization code required")
		return
	}

	// In production, this would exchange the code via OIDCProvider.ExchangeCode
	Respond(w, 200, map[string]string{
		"message": "OIDC callback received",
		"code":    code,
		"state":   state,
		"status":  "not_configured",
	})
}
