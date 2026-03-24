package api

import (
	"net/http"
	"os"
)

func (s *Server) handleBranding(w http.ResponseWriter, r *http.Request) {
	branding := map[string]string{
		"name":    getEnvOrDefault("CYNTR_BRAND_NAME", "Cyntr"),
		"tagline": getEnvOrDefault("CYNTR_BRAND_TAGLINE", "Agent OS"),
		"accent":  getEnvOrDefault("CYNTR_BRAND_ACCENT", "#6366f1"),
		"logo":    getEnvOrDefault("CYNTR_BRAND_LOGO", ""),
	}
	Respond(w, 200, branding)
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
