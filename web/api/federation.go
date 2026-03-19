package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/federation"
)

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
