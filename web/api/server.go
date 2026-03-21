package api

import (
	"net/http"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/tenant"
)

// Server is the REST API server.
type Server struct {
	mux       *http.ServeMux
	bus       *ipc.Bus
	kernel    *kernel.Kernel
	tenantMgr *tenant.Manager
}

// SetTenantManager sets the tenant manager after construction.
// This preserves backward compatibility with existing NewServer callers.
func (s *Server) SetTenantManager(tm *tenant.Manager) {
	s.tenantMgr = tm
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
	s.mux.ServeHTTP(w, r)
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
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents/{name}", s.handleAgentGet)
	s.mux.HandleFunc("DELETE /api/v1/tenants/{tid}/agents/{name}", s.handleAgentDelete)
	s.mux.HandleFunc("PUT /api/v1/tenants/{tid}/agents/{name}", s.handleAgentUpdate)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents/{name}/sessions", s.handleAgentSessions)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents/{name}/sessions/{sid}/messages", s.handleSessionMessages)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents/{name}/memories", s.handleAgentMemories)
	s.mux.HandleFunc("DELETE /api/v1/tenants/{tid}/agents/{name}/memories/{mid}", s.handleMemoryDelete)

	// Policies
	s.mux.HandleFunc("POST /api/v1/policies/test", s.handlePolicyTest)
	s.mux.HandleFunc("GET /api/v1/policies/rules", s.handlePolicyRulesList)

	// Skills
	s.mux.HandleFunc("GET /api/v1/skills", s.handleSkillList)
	s.mux.HandleFunc("POST /api/v1/skills", s.handleSkillInstall)
	s.mux.HandleFunc("POST /api/v1/skills/import/openclaw", s.handleSkillImportOpenClaw)
	s.mux.HandleFunc("GET /api/v1/skills/marketplace/search", s.handleSkillMarketplaceSearch)
	s.mux.HandleFunc("GET /api/v1/skills/marketplace", s.handleSkillMarketplaceSearch)
	s.mux.HandleFunc("DELETE /api/v1/skills/{name}", s.handleSkillUninstall)

	// Audit
	s.mux.HandleFunc("GET /api/v1/audit", s.handleAuditQuery)

	// Channels
	s.mux.HandleFunc("GET /api/v1/channels", s.handleChannelList)

	// Federation
	s.mux.HandleFunc("GET /api/v1/federation/peers", s.handleFederationPeers)
	s.mux.HandleFunc("POST /api/v1/federation/peers", s.handleFederationJoin)
	s.mux.HandleFunc("DELETE /api/v1/federation/peers/{name}", s.handleFederationRemove)

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
	s.mux.HandleFunc("POST /api/v1/knowledge", s.handleKnowledgeIngest)
	s.mux.HandleFunc("DELETE /api/v1/knowledge/{id}", s.handleKnowledgeDelete)

	// Webhooks
	s.mux.HandleFunc("POST /api/v1/webhooks/trigger/{workflow_id}", s.handleWebhookTrigger)
}
