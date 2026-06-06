package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/audit"
)

func (s *Server) handleAuditQuery(w http.ResponseWriter, r *http.Request) {
	filter := audit.QueryFilter{
		Tenant:     r.URL.Query().Get("tenant"),
		ActionType: r.URL.Query().Get("action"),
		User:       r.URL.Query().Get("user"),
		Agent:      r.URL.Query().Get("agent"),
	}
	// Scope to the authenticated principal's tenant so a caller cannot read
	// another tenant's audit log via ?tenant=. The store also rejects an empty
	// tenant, so a scopeless query errors rather than returning all tenants.
	if p, ok := authPrincipal(r); ok && p.Tenant != "" {
		filter.Tenant = p.Tenant
	}
	if filter.Tenant == "" {
		RespondError(w, 400, "MISSING_TENANT", "tenant is required (query param or authenticated identity)")
		return
	}

	if since := r.URL.Query().Get("since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			filter.Since = t
		}
	}
	if until := r.URL.Query().Get("until"); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			filter.Until = t
		}
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		if n, err := strconv.Atoi(limit); err == nil {
			filter.Limit = n
		}
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
