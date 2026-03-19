package api

import (
	"context"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/audit"
)

func (s *Server) handleAuditQuery(w http.ResponseWriter, r *http.Request) {
	filter := audit.QueryFilter{
		Tenant:     r.URL.Query().Get("tenant"),
		ActionType: r.URL.Query().Get("action"),
		User:       r.URL.Query().Get("user"),
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "audit", Topic: "audit.query",
		Payload: filter,
	})
	if err != nil {
		RespondError(w, 500, "AUDIT_ERROR", err.Error())
		return
	}

	Respond(w, 200, resp.Payload)
}
