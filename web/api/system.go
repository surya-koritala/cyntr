package api

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) handleSystemHealth(w http.ResponseWriter, r *http.Request) {
	if s.kernel == nil {
		Respond(w, 200, map[string]string{"status": "ok"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	report := s.kernel.HealthReport(ctx)
	Respond(w, 200, report)
}

// Version is set by the server creator.
var Version = "0.8.0"

func (s *Server) handleSystemVersion(w http.ResponseWriter, r *http.Request) {
	Respond(w, 200, map[string]string{"version": Version})
}
