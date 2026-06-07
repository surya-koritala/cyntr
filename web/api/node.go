package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/node"
)

// handleNodePairIssue issues a pairing code for a companion node (B10).
//
//	POST /api/v1/node/pair  {"node":"laptop","capabilities":["voice","canvas"]}
//
// The tenant is taken from the authenticated principal when present (a caller
// cannot mint a code for another tenant); in an unauthenticated dev deployment
// it falls back to the request body's "tenant" or "default".
func (s *Server) handleNodePairIssue(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Node         string   `json:"node"`
		Tenant       string   `json:"tenant"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		RespondError(w, 400, "BAD_REQUEST", "invalid JSON body")
		return
	}
	if body.Node == "" {
		RespondError(w, 400, "MISSING_NODE", "node is required")
		return
	}

	tenant := body.Tenant
	if p, ok := authPrincipal(r); ok && p.Tenant != "" {
		tenant = p.Tenant // authenticated identity wins
	}
	if tenant == "" {
		tenant = "default"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "node", Topic: "node.pair.issue",
		Payload: node.IssueRequest{Tenant: tenant, Node: body.Node, Capabilities: body.Capabilities},
	})
	if err != nil {
		RespondError(w, 500, "NODE_PAIR_ERROR", err.Error())
		return
	}
	out, ok := resp.Payload.(node.IssueResponse)
	if !ok {
		RespondError(w, 500, "NODE_PAIR_ERROR", "unexpected response from node module")
		return
	}
	if out.Error != "" {
		RespondError(w, 400, "NODE_PAIR_ERROR", out.Error)
		return
	}
	Respond(w, 200, map[string]any{"code": out.Code, "node": body.Node, "tenant": tenant, "capabilities": body.Capabilities})
}
