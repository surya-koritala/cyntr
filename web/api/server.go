package api

import (
	"net/http"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Server is the REST API server.
type Server struct {
	mux    *http.ServeMux
	bus    *ipc.Bus
	kernel *kernel.Kernel
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

	// Tenants
	s.mux.HandleFunc("GET /api/v1/tenants", s.handleTenantList)

	// Agents
	s.mux.HandleFunc("POST /api/v1/tenants/{tid}/agents", s.handleAgentCreate)
	s.mux.HandleFunc("POST /api/v1/tenants/{tid}/agents/{name}/chat", s.handleAgentChat)
	s.mux.HandleFunc("GET /api/v1/tenants/{tid}/agents/{name}/stream", s.handleAgentChatStream)

	// Policies
	s.mux.HandleFunc("POST /api/v1/policies/test", s.handlePolicyTest)

	// Skills
	s.mux.HandleFunc("GET /api/v1/skills", s.handleSkillList)
	s.mux.HandleFunc("POST /api/v1/skills", s.handleSkillInstall)
	s.mux.HandleFunc("POST /api/v1/skills/import/openclaw", s.handleSkillImportOpenClaw)

	// Audit
	s.mux.HandleFunc("GET /api/v1/audit", s.handleAuditQuery)

	// Federation
	s.mux.HandleFunc("GET /api/v1/federation/peers", s.handleFederationPeers)
	s.mux.HandleFunc("POST /api/v1/federation/peers", s.handleFederationJoin)

	// Approvals
	s.mux.HandleFunc("GET /api/v1/approvals", s.handleApprovalList)
	s.mux.HandleFunc("POST /api/v1/approvals/{id}/approve", s.handleApprovalApprove)
	s.mux.HandleFunc("POST /api/v1/approvals/{id}/deny", s.handleApprovalDeny)

	// Auth
	s.mux.HandleFunc("GET /api/v1/auth/oidc/login", s.handleOIDCLogin)
	s.mux.HandleFunc("GET /api/v1/auth/oidc/callback", s.handleOIDCCallback)
}
