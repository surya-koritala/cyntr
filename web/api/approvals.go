package api

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleApprovalList(w http.ResponseWriter, r *http.Request) {
	// For now, return empty list — approval queue integration comes next
	Respond(w, 200, []any{})
}

func (s *Server) handleApprovalApprove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		DecidedBy string `json:"decided_by"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	Respond(w, 200, map[string]string{"id": id, "status": "approved", "decided_by": body.DecidedBy})
}

func (s *Server) handleApprovalDeny(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		DecidedBy string `json:"decided_by"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	Respond(w, 200, map[string]string{"id": id, "status": "denied", "decided_by": body.DecidedBy})
}
