package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/mcp"
)

func (s *Server) handleMCPServerList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "mcp", Topic: "mcp.server.list",
	})
	if err != nil {
		Respond(w, 200, []any{})
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleMCPServerAdd(w http.ResponseWriter, r *http.Request) {
	var body mcp.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	if body.Name == "" || body.Transport == "" {
		RespondError(w, 400, "MISSING_FIELDS", "name and transport are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "mcp", Topic: "mcp.server.add",
		Payload: body,
	})
	if err != nil {
		RespondError(w, 500, "CONNECT_FAILED", err.Error())
		return
	}
	Respond(w, 201, map[string]string{"status": "connected", "name": body.Name})
}

func (s *Server) handleMCPServerRemove(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "mcp", Topic: "mcp.server.remove",
		Payload: name,
	})
	if err != nil {
		RespondError(w, 500, "REMOVE_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"status": "disconnected", "name": name})
}

func (s *Server) handleMCPServerTools(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "mcp", Topic: "mcp.server.tools",
		Payload: name,
	})
	if err != nil {
		RespondError(w, 404, "NOT_FOUND", err.Error())
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleMCPMarketplaceSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "mcp", Topic: "mcp.marketplace.search",
		Payload: query,
	})
	if err != nil {
		Respond(w, 200, []any{})
		return
	}
	Respond(w, 200, resp.Payload)
}

func (s *Server) handleMCPMarketplaceInstall(w http.ResponseWriter, r *http.Request) {
	var body mcp.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	// Same as add — install means connect
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "mcp", Topic: "mcp.server.add",
		Payload: body,
	})
	if err != nil {
		RespondError(w, 500, "INSTALL_FAILED", err.Error())
		return
	}
	Respond(w, 201, map[string]string{"status": "installed", "name": body.Name})
}
