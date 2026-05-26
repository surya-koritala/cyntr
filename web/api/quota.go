package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/quota"
)

// quotaResponse is the shape returned by GET /api/v1/tenants/{tid}/quota.
type quotaResponse struct {
	Config quota.QuotaConfig `json:"config"`
	Usage  quota.Usage       `json:"usage"`
}

func (s *Server) handleQuotaGet(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tid")
	if tenant == "" {
		RespondError(w, 400, "INVALID_REQUEST", "tenant id required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	// Config
	cfgResp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: quota.ModuleName, Topic: quota.TopicConfigGet, Payload: tenant,
	})
	if err == ipc.ErrNoHandler {
		// Quota module not registered → everything is unlimited. Return the
		// default config so clients can render a "no limits" UI.
		Respond(w, 200, quotaResponse{Config: quota.QuotaConfig{Tenant: tenant}})
		return
	}
	if err != nil {
		RespondError(w, 500, "QUOTA_ERROR", err.Error())
		return
	}
	cfg, _ := cfgResp.Payload.(quota.QuotaConfig)

	// Usage
	usageResp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: quota.ModuleName, Topic: quota.TopicUsage, Payload: tenant,
	})
	var u quota.Usage
	if err == nil {
		u, _ = usageResp.Payload.(quota.Usage)
	}
	Respond(w, 200, quotaResponse{Config: cfg, Usage: u})
}

func (s *Server) handleQuotaSet(w http.ResponseWriter, r *http.Request) {
	tenant := r.PathValue("tid")
	if tenant == "" {
		RespondError(w, 400, "INVALID_REQUEST", "tenant id required")
		return
	}
	var cfg quota.QuotaConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	// Path parameter wins over body — keeps the URL canonical.
	cfg.Tenant = tenant

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if _, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: quota.ModuleName, Topic: quota.TopicConfigSet, Payload: cfg,
	}); err != nil {
		if err == ipc.ErrNoHandler {
			RespondError(w, 503, "QUOTA_DISABLED", "quota module is not enabled")
			return
		}
		RespondError(w, 500, "QUOTA_ERROR", err.Error())
		return
	}
	Respond(w, 200, cfg)
}
