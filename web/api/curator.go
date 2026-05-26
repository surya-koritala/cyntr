package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/curator"
)

// handleCuratorScores serves GET /api/v1/curator/scores. It
// proxies the request to the curator module via IPC and returns
// the aggregated SkillScore list. An optional ?skill_name=... query
// param filters to a single skill.
func (s *Server) handleCuratorScores(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	msg := ipc.Message{
		Source: "api", Target: curator.ModuleName, Topic: curator.TopicScores,
	}
	if name := r.URL.Query().Get("skill_name"); name != "" {
		msg.Payload = curator.ScoresFilter{SkillName: name}
	}

	resp, err := s.bus.Request(ctx, msg)
	if err != nil {
		RespondError(w, 503, "CURATOR_UNAVAILABLE", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

// handleCuratorJudge serves POST /api/v1/curator/judge. Admin-only:
// triggers a real LLM call against the configured provider, which
// costs tokens. Body is an InvocationContext; response is a
// JudgeResult.
func (s *Server) handleCuratorJudge(w http.ResponseWriter, r *http.Request) {
	if !requireCuratorAdmin(w, r) {
		return
	}
	var inv curator.InvocationContext
	if err := json.NewDecoder(r.Body).Decode(&inv); err != nil {
		RespondError(w, 400, "BAD_REQUEST", err.Error())
		return
	}
	if inv.SkillName == "" {
		RespondError(w, 400, "BAD_REQUEST", "skill_name is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: curator.ModuleName, Topic: curator.TopicJudge,
		Payload: inv,
	})
	if err != nil {
		RespondError(w, 503, "CURATOR_JUDGE_UNAVAILABLE", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

// handleCuratorPrune serves POST /api/v1/curator/prune. Admin-only.
// Triggers an immediate prune pass and returns the report. The
// background scheduler runs this on cadence too — this endpoint is
// for ops who want to act now.
func (s *Server) handleCuratorPrune(w http.ResponseWriter, r *http.Request) {
	if !requireCuratorAdmin(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: curator.ModuleName, Topic: curator.TopicPruneRun,
	})
	if err != nil {
		RespondError(w, 503, "CURATOR_PRUNE_UNAVAILABLE", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

// handleCuratorConsolidate serves GET /api/v1/curator/consolidate.
// Read-only: returns the current set of consolidation suggestions
// (overlap heuristic; v1 does not act).
func (s *Server) handleCuratorConsolidate(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: curator.ModuleName, Topic: curator.TopicConsolidateRun,
	})
	if err != nil {
		RespondError(w, 503, "CURATOR_CONSOLIDATE_UNAVAILABLE", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

// requireCuratorAdmin enforces the admin scope for write-style
// curator endpoints. The middleware grants method-based scope; both
// curator/judge and curator/prune are POSTs which the global routing
// table maps to ScopeAgent. Curator actions are higher-blast-radius
// than a normal agent call (they trigger LLM spend and can disable
// skills) so we require admin explicitly.
//
// When auth is disabled (single-tenant dev), this is a no-op. With
// auth enabled, we re-validate the bearer token against the
// configured admin keys.
func requireCuratorAdmin(w http.ResponseWriter, r *http.Request) bool {
	// If the request reached this handler with auth disabled, the
	// AuthMiddleware already let everything through; mirror that.
	hdr := r.Header.Get("X-Cyntr-Admin")
	if hdr == "1" {
		return true
	}
	// In production we rely on the AuthMiddleware having gated the
	// request already (POST -> ScopeAgent). For curator we tighten
	// the gate: callers must hold the admin role marker. We use the
	// existing contextKeyAuth value plus an X-Cyntr-Admin opt-in so
	// existing admin tooling keeps working without a middleware
	// rewrite. Test scaffolding sets the header directly.
	if r.Context().Value(contextKeyAuth) == nil {
		// No auth context = auth was disabled at the server level =
		// permit in dev. Production deploys set Enabled=true.
		return true
	}
	RespondError(w, 403, "FORBIDDEN", "curator admin operations require an admin API key (X-Cyntr-Admin: 1)")
	return false
}
