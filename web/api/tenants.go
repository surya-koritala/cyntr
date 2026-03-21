package api

import (
	"encoding/json"
	"net/http"

	"github.com/cyntr-dev/cyntr/tenant"
)

func (s *Server) handleTenantList(w http.ResponseWriter, r *http.Request) {
	if s.kernel == nil {
		Respond(w, 200, []string{})
		return
	}

	cfg := s.kernel.Config().Get()
	tenants := make([]map[string]string, 0)
	for name, tc := range cfg.Tenants {
		tenants = append(tenants, map[string]string{
			"name":      name,
			"isolation": tc.Isolation,
			"policy":    tc.Policy,
		})
	}
	Respond(w, 200, tenants)
}

func (s *Server) handleTenantGet(w http.ResponseWriter, r *http.Request) {
	if s.tenantMgr == nil {
		RespondError(w, 500, "NOT_CONFIGURED", "tenant manager not configured")
		return
	}

	tid := r.PathValue("tid")
	t, ok := s.tenantMgr.Get(tid)
	if !ok {
		RespondError(w, 404, "NOT_FOUND", "tenant not found")
		return
	}

	Respond(w, 200, map[string]string{
		"name":      t.Name,
		"isolation": t.Isolation.String(),
		"policy":    t.Policy,
	})
}

func (s *Server) handleTenantCreate(w http.ResponseWriter, r *http.Request) {
	if s.tenantMgr == nil {
		RespondError(w, 500, "NOT_CONFIGURED", "tenant manager not configured")
		return
	}

	var body struct {
		Name      string `json:"name"`
		Isolation string `json:"isolation"`
		Policy    string `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	var mode tenant.IsolationMode
	switch body.Isolation {
	case "process":
		mode = tenant.IsolationProcess
	default:
		mode = tenant.IsolationNamespace
	}

	if err := s.tenantMgr.Create(body.Name, mode, body.Policy); err != nil {
		RespondError(w, 409, "CREATE_FAILED", err.Error())
		return
	}

	Respond(w, 201, map[string]string{"status": "created", "name": body.Name})
}

func (s *Server) handleTenantDelete(w http.ResponseWriter, r *http.Request) {
	if s.tenantMgr == nil {
		RespondError(w, 500, "NOT_CONFIGURED", "tenant manager not configured")
		return
	}

	tid := r.PathValue("tid")
	if err := s.tenantMgr.Delete(tid); err != nil {
		RespondError(w, 404, "DELETE_FAILED", err.Error())
		return
	}

	Respond(w, 200, map[string]string{"status": "deleted", "name": tid})
}
