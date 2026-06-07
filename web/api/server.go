package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/notify"
	"github.com/cyntr-dev/cyntr/modules/observability"
	"github.com/cyntr-dev/cyntr/tenant"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// httpTracer is the tracer used for inbound HTTP requests served by Server.
// When observability is disabled this is a no-op tracer; spans cost ~nothing.
var httpTracer = observability.Tracer("github.com/cyntr-dev/cyntr/web/api")

// Server is the REST API server.
type Server struct {
	mux       *http.ServeMux
	bus       *ipc.Bus
	kernel    *kernel.Kernel
	tenantMgr *tenant.Manager
	notifier  *notify.Notifier
	// promHandler, if non-nil, is served at /api/v1/metrics/prom. The
	// observability module installs it via SetPrometheusHandler when an OTLP
	// endpoint is configured; otherwise the endpoint returns 404.
	promHandler http.Handler
	// voice holds the STT/TTS tools for the voice round-trip endpoint (B8).
	// nil => the endpoint lazily builds vendor/gateway-backed tools per call.
	// Tests inject mock-backed tools via SetVoiceTools.
	voice *voiceTools
}

// SetTenantManager sets the tenant manager after construction.
// This preserves backward compatibility with existing NewServer callers.
func (s *Server) SetTenantManager(tm *tenant.Manager) {
	s.tenantMgr = tm
}

// SetNotifier sets the notification manager.
func (s *Server) SetNotifier(n *notify.Notifier) {
	s.notifier = n
}

// SetPrometheusHandler wires the OTel-backed Prometheus exposition handler.
// Pass nil to disable the /api/v1/metrics/prom endpoint.
func (s *Server) SetPrometheusHandler(h http.Handler) {
	s.promHandler = h
}

// handleMetricsProm serves the OTel Prometheus exposition, or 404 if the
// observability module isn't enabled (in which case no handler is installed).
func (s *Server) handleMetricsProm(w http.ResponseWriter, r *http.Request) {
	if s.promHandler == nil {
		RespondError(w, http.StatusNotFound, "OBSERVABILITY_DISABLED",
			"Prometheus exposition requires OTEL_EXPORTER_OTLP_ENDPOINT to be set")
		return
	}
	s.promHandler.ServeHTTP(w, r)
}

// NewServer creates an API server wired to the kernel IPC bus.
func NewServer(bus *ipc.Bus, k *kernel.Kernel) *Server {
	s := &Server{
		mux:    http.NewServeMux(),
		bus:    bus,
		kernel: k,
	}
	s.registerRoutes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// http.request span wraps every API call. Path is the raw request path —
	// for routes with parameters this is high-cardinality, but for v0 we
	// accept that; templating requires running through the mux first and
	// pulling the matched pattern, which net/http doesn't expose cleanly.
	ctx, span := httpTracer.Start(r.Context(), "http.request")
	span.SetAttributes(
		attribute.String("method", r.Method),
		attribute.String("path", r.URL.Path),
	)

	// Capture the status code via a wrapper. We deliberately don't reuse
	// statusWriter from metrics.go to keep the dependency explicit and to
	// avoid coupling the two middlewares' internals.
	tw := &tracingWriter{ResponseWriter: w, status: http.StatusOK}
	start := time.Now()
	s.mux.ServeHTTP(tw, r.WithContext(ctx))
	span.SetAttributes(
		attribute.Int("status_code", tw.status),
		attribute.Int64("duration_ms", time.Since(start).Milliseconds()),
	)
	if tw.status >= 500 {
		span.SetStatus(codes.Error, strconv.Itoa(tw.status))
	}
	span.End()
}

// tracingWriter captures the response status code for the http.request span.
// It also forwards Flush() so SSE streaming routes keep working.
type tracingWriter struct {
	http.ResponseWriter
	status int
}

func (w *tracingWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *tracingWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) registerRoutes() {
	// System
	s.mux.HandleFunc("GET /api/v1/system/health", s.handleSystemHealth)
	s.mux.HandleFunc("GET /api/v1/system/version", s.handleSystemVersion)

	// Tenants (CRUD)
	s.mux.HandleFunc("GET /api/v1/tenants", s.handleTenantList)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}", s.handleTenantGet)
	s.mux.HandleFunc("POST /api/v1/tenants", s.handleTenantCreate)
	s.mux.HandleFunc("DELETE /api/v1/tenants/{tid}", s.handleTenantDelete)

	// Agents
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents", s.handleAgentList)
	s.mux.HandleFunc("POST /api/v1/tenants/{tid}/agents", s.handleAgentCreate)
	s.mux.HandleFunc("POST /api/v1/tenants/{tid}/agents/{name}/chat", s.handleAgentChat)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents/{name}/stream", s.handleAgentChatStream)
	// Voice round-trip (B8): audio -> STT -> agent turn -> TTS -> audio.
	s.mux.HandleFunc("POST /api/v1/tenants/{tid}/agents/{name}/voice", s.handleAgentVoice)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents/{name}", s.handleAgentGet)
	s.mux.HandleFunc("DELETE /api/v1/tenants/{tid}/agents/{name}", s.handleAgentDelete)
	s.mux.HandleFunc("PUT /api/v1/tenants/{tid}/agents/{name}", s.handleAgentUpdate)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents/{name}/sessions", s.handleAgentSessions)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents/{name}/sessions/{sid}/messages", s.handleSessionMessages)
	s.mux.HandleFunc("DELETE /api/v1/tenants/{tid}/agents/{name}/sessions/{sid}", s.handleSessionClear)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents/{name}/memories", s.handleAgentMemories)
	s.mux.HandleFunc("DELETE /api/v1/tenants/{tid}/agents/{name}/memories/{mid}", s.handleMemoryDelete)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents/{name}/versions", s.handleAgentVersions)
	s.mux.HandleFunc("POST /api/v1/tenants/{tid}/agents/{name}/rollback/{version}", s.handleAgentRollback)

	// Users
	s.mux.HandleFunc("POST /api/v1/tenants/{tid}/users", s.handleUserCreate)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/users", s.handleUserList)
	s.mux.HandleFunc("DELETE /api/v1/tenants/{tid}/users/{uid}", s.handleUserDelete)
	s.mux.HandleFunc("GET /api/v1/auth/me", s.handleAuthMe)

	// User curated profile (USER.md / SOUL.md equivalent)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/users/{uid}/profile", s.handleUserProfileGet)
	s.mux.HandleFunc("PUT /api/v1/tenants/{tid}/users/{uid}/profile", s.handleUserProfilePut)
	s.mux.HandleFunc("POST /api/v1/tenants/{tid}/users/{uid}/profile/distill", s.handleUserProfileDistill)

	// Search
	s.mux.HandleFunc("GET /api/v1/search", s.handleAgentSearch)

	// Policies
	s.mux.HandleFunc("POST /api/v1/policies/test", s.handlePolicyTest)
	s.mux.HandleFunc("GET /api/v1/policies/rules", s.handlePolicyRulesList)

	// Skills
	s.mux.HandleFunc("GET /api/v1/skills", s.handleSkillList)
	s.mux.HandleFunc("POST /api/v1/skills", s.handleSkillInstall)
	// Autonomous skill proposals (A2): review queue + approve/reject.
	s.mux.HandleFunc("GET /api/v1/skills/candidates", s.handleSkillCandidates)
	s.mux.HandleFunc("POST /api/v1/skills/candidates/{id}/approve", s.handleSkillCandidateApprove)
	s.mux.HandleFunc("POST /api/v1/skills/candidates/{id}/reject", s.handleSkillCandidateReject)
	s.mux.HandleFunc("POST /api/v1/skills/import/openclaw", s.handleSkillImportOpenClaw)
	s.mux.HandleFunc("GET /api/v1/skills/marketplace/search", s.handleSkillMarketplaceSearch)
	s.mux.HandleFunc("GET /api/v1/skills/marketplace", s.handleSkillMarketplaceSearch)
	s.mux.HandleFunc("POST /api/v1/skills/marketplace/install", s.handleSkillMarketplaceInstall)
	s.mux.HandleFunc("DELETE /api/v1/skills/{name}", s.handleSkillUninstall)

	// Audit
	s.mux.HandleFunc("GET /api/v1/audit", s.handleAuditQuery)

	// Channels
	s.mux.HandleFunc("GET /api/v1/channels", s.handleChannelList)

	// Federation
	s.mux.HandleFunc("GET /api/v1/federation/peers", s.handleFederationPeers)
	s.mux.HandleFunc("POST /api/v1/federation/peers", s.handleFederationJoin)
	s.mux.HandleFunc("DELETE /api/v1/federation/peers/{name}", s.handleFederationRemove)
	s.mux.HandleFunc("POST /api/v1/federation/delegate", s.handleFederationDelegate)
	// Inbound endpoints hit by peer nodes.
	s.mux.HandleFunc("POST /api/v1/federation/inbound/delegate", s.handleFederationDelegateInbound)
	s.mux.HandleFunc("GET /api/v1/federation/health", s.handleFederationHealth)

	// Approvals
	s.mux.HandleFunc("GET /api/v1/approvals", s.handleApprovalList)
	s.mux.HandleFunc("POST /api/v1/approvals/{id}/approve", s.handleApprovalApprove)
	s.mux.HandleFunc("POST /api/v1/approvals/{id}/deny", s.handleApprovalDeny)

	// Auth
	s.mux.HandleFunc("GET /api/v1/auth/oidc/login", s.handleOIDCLogin)
	s.mux.HandleFunc("GET /api/v1/auth/oidc/callback", s.handleOIDCCallback)

	// Workflows
	s.mux.HandleFunc("POST /api/v1/workflows", s.handleWorkflowRegister)
	s.mux.HandleFunc("GET /api/v1/workflows", s.handleWorkflowList)
	s.mux.HandleFunc("GET /api/v1/workflows/{id}", s.handleWorkflowGet)
	s.mux.HandleFunc("POST /api/v1/workflows/{id}/run", s.handleWorkflowRun)
	s.mux.HandleFunc("GET /api/v1/workflows/runs", s.handleWorkflowListRuns)
	s.mux.HandleFunc("GET /api/v1/workflows/runs/{id}", s.handleWorkflowRunStatus)

	// Schedules
	s.mux.HandleFunc("POST /api/v1/schedules", s.handleScheduleAdd)
	s.mux.HandleFunc("GET /api/v1/schedules", s.handleScheduleList)
	s.mux.HandleFunc("POST /api/v1/schedules/{id}/remove", s.handleScheduleRemove)

	// Knowledge base
	s.mux.HandleFunc("GET /api/v1/knowledge", s.handleKnowledgeList)
	s.mux.HandleFunc("GET /api/v1/knowledge/search", s.handleKnowledgeSearch)
	s.mux.HandleFunc("POST /api/v1/knowledge", s.handleKnowledgeIngest)
	s.mux.HandleFunc("DELETE /api/v1/knowledge/{id}", s.handleKnowledgeDelete)

	// Webhooks
	s.mux.HandleFunc("POST /api/v1/webhooks/trigger/{workflow_id}", s.handleWebhookTrigger)
	s.mux.HandleFunc("POST /api/v1/webhooks/agent/{tenant}/{agent}", s.handleWebhookAgent)

	// Branding
	s.mux.HandleFunc("GET /api/v1/branding", s.handleBranding)

	// Metrics
	s.mux.HandleFunc("GET /api/v1/metrics", s.handleMetrics)
	// Prometheus exposition of OTel-managed metrics (cyntr.* instruments) lives
	// at a separate path so the existing JSON metrics endpoint stays
	// backward-compatible. Returns 404 until SetPrometheusHandler is called by
	// main, which only happens when the observability module is enabled.
	s.mux.HandleFunc("GET /api/v1/metrics/prom", s.handleMetricsProm)

	// MCP Servers
	s.mux.HandleFunc("GET /api/v1/mcp/servers", s.handleMCPServerList)
	s.mux.HandleFunc("POST /api/v1/mcp/servers", s.handleMCPServerAdd)
	s.mux.HandleFunc("DELETE /api/v1/mcp/servers/{name}", s.handleMCPServerRemove)
	s.mux.HandleFunc("GET /api/v1/mcp/servers/{name}/tools", s.handleMCPServerTools)
	s.mux.HandleFunc("GET /api/v1/mcp/marketplace", s.handleMCPMarketplaceSearch)
	s.mux.HandleFunc("POST /api/v1/mcp/marketplace/install", s.handleMCPMarketplaceInstall)

	// Crews
	s.mux.HandleFunc("POST /api/v1/crews", s.handleCrewCreate)
	s.mux.HandleFunc("GET /api/v1/crews", s.handleCrewList)
	s.mux.HandleFunc("POST /api/v1/crews/{id}/run", s.handleCrewRun)
	s.mux.HandleFunc("GET /api/v1/crews/runs/{run_id}", s.handleCrewRunStatus)
	s.mux.HandleFunc("GET /api/v1/crews/runs", s.handleCrewListRuns)

	// Eval
	s.mux.HandleFunc("POST /api/v1/eval/run", s.handleEvalRun)
	s.mux.HandleFunc("GET /api/v1/eval/runs/{id}", s.handleEvalStatus)
	s.mux.HandleFunc("GET /api/v1/eval/runs", s.handleEvalList)

	// Curator (F3 — skill scoring & prune suggestions; T3.6 — judge/prune/consolidate)
	s.mux.HandleFunc("GET /api/v1/curator/scores", s.handleCuratorScores)
	s.mux.HandleFunc("POST /api/v1/curator/judge", s.handleCuratorJudge)
	s.mux.HandleFunc("POST /api/v1/curator/prune", s.handleCuratorPrune)
	s.mux.HandleFunc("GET /api/v1/curator/consolidate", s.handleCuratorConsolidate)

	// Usage
	s.mux.HandleFunc("GET /api/v1/usage", s.handleUsageQuery)
	s.mux.HandleFunc("GET /api/v1/usage/summary", s.handleUsageSummary)

	// Quotas (per-tenant token/rate/concurrency/session caps)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/quota", s.handleQuotaGet)
	s.mux.HandleFunc("PUT /api/v1/tenants/{tid}/quota", s.handleQuotaSet)

	// SLA Monitoring
	s.mux.HandleFunc("POST /api/v1/sla/rules", s.handleSLAAddRule)
	s.mux.HandleFunc("GET /api/v1/sla/rules", s.handleSLAListRules)
	s.mux.HandleFunc("DELETE /api/v1/sla/rules/{id}", s.handleSLARemoveRule)
	s.mux.HandleFunc("GET /api/v1/sla/violations", s.handleSLAViolations)

	// Notifications
	s.mux.HandleFunc("GET /api/v1/notifications/channels", s.handleNotificationChannels)
	s.mux.HandleFunc("POST /api/v1/notifications/test", s.handleNotificationTest)

	// Observability
	s.mux.HandleFunc("GET /api/v1/observability/latency", s.handleObservabilityLatency)
	s.mux.HandleFunc("GET /api/v1/observability/tokens", s.handleObservabilityTokens)
	s.mux.HandleFunc("GET /api/v1/observability/tools", s.handleObservabilityTools)

	// Tools
	s.mux.HandleFunc("GET /api/v1/tools", s.handleToolList)
	s.mux.HandleFunc("POST /api/v1/tools", s.handleToolCreate)
	s.mux.HandleFunc("DELETE /api/v1/tools/{name}", s.handleToolDelete)
}
