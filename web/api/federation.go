package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/federation"
)

func (s *Server) handleFederationRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "federation", Topic: "federation.remove",
		Payload: name,
	})
	if err != nil {
		RespondError(w, 500, "REMOVE_FAILED", err.Error())
		return
	}

	Respond(w, 200, map[string]string{"status": "removed", "name": name})
}

func (s *Server) handleFederationPeers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "federation", Topic: "federation.peers",
	})
	if err != nil {
		RespondError(w, 500, "FEDERATION_ERROR", err.Error())
		return
	}

	Respond(w, 200, resp.Payload)
}

func (s *Server) handleFederationJoin(w http.ResponseWriter, r *http.Request) {
	var body federation.Peer
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "federation", Topic: "federation.join",
		Payload: body,
	})
	if err != nil {
		RespondError(w, 500, "JOIN_FAILED", err.Error())
		return
	}

	Respond(w, 201, map[string]string{"status": "joined", "peer": body.Name})
}

// handleFederationDelegate sends an outbound delegation request to a registered peer.
// Body: federation.DelegateRequest (peer, tenant, agent, user, message).
func (s *Server) handleFederationDelegate(w http.ResponseWriter, r *http.Request) {
	var body federation.DelegateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	if body.Peer == "" || body.Tenant == "" || body.Agent == "" || body.Message == "" {
		RespondError(w, 400, "INVALID_REQUEST", "peer, tenant, agent, and message are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "federation", Topic: "federation.delegate",
		Payload: body,
	})
	if err != nil {
		RespondError(w, 502, "DELEGATE_FAILED", err.Error())
		return
	}

	delegateResp, ok := resp.Payload.(federation.DelegateResponse)
	if !ok {
		RespondError(w, 500, "INTERNAL", "unexpected response type")
		return
	}
	Respond(w, 200, delegateResp)
}

// handleFederationDelegateInbound is the peer-facing endpoint. A remote node
// POSTs a DelegateRequest here; we dispatch through the federation module so
// local policy is enforced before the agent runtime runs.
func (s *Server) handleFederationDelegateInbound(w http.ResponseWriter, r *http.Request) {
	var body federation.DelegateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	// Carry the shared secret from the header (never the body) so the
	// federation module can authenticate the calling peer.
	body.Secret = r.Header.Get("X-Federation-Secret")

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "federation", Topic: "federation.delegate.inbound",
		Payload: body,
	})
	if err != nil {
		RespondError(w, 500, "INBOUND_FAILED", err.Error())
		return
	}

	delegateResp, ok := resp.Payload.(federation.DelegateResponse)
	if !ok {
		RespondError(w, 500, "INTERNAL", "unexpected response type")
		return
	}
	Respond(w, 200, delegateResp)
}

// handleFederationHealth returns a lightweight liveness probe for peers.
func (s *Server) handleFederationHealth(w http.ResponseWriter, r *http.Request) {
	Respond(w, 200, map[string]string{"status": "ok"})
}
