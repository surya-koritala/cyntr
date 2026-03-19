package api

import (
	"net/http"
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
