package mcp

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// Server exposes Cyntr tools as an MCP server.
type Server struct {
	toolReg *agent.ToolRegistry
	codec   *Codec
	// authToken gates tools/list and tools/call. It is loaded from
	// CYNTR_MCP_SERVER_TOKEN. When empty the server is fail-closed: the
	// handshake is allowed but no tools are listed or executed, so an
	// operator must explicitly provision a token to expose the registry.
	authToken string
	// authTenant optionally binds the configured token to a tenant
	// (CYNTR_MCP_SERVER_TENANT) so tool execution runs with that caller's
	// scope rather than as an unscoped global principal.
	authTenant string
}

func NewServer(toolReg *agent.ToolRegistry) *Server {
	return &Server{
		toolReg:    toolReg,
		codec:      NewCodec(),
		authToken:  os.Getenv("CYNTR_MCP_SERVER_TOKEN"),
		authTenant: os.Getenv("CYNTR_MCP_SERVER_TENANT"),
	}
}

// authError is the JSON-RPC error code used when a request is not authorized.
const authError = -32001

// checkToken reports whether presented matches the configured token in
// constant time. It fails closed when no token is configured.
func (s *Server) checkToken(presented string) bool {
	if s.authToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(s.authToken)) == 1
}

// ServeStdio runs the MCP server on stdin/stdout. The stdio transport carries
// no per-request credentials, so the whole session is authenticated once from
// CYNTR_MCP_SERVER_TOKEN: with no token configured the server is fail-closed
// and exposes no tools.
func (s *Server) ServeStdio(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	authed := s.authToken != ""

	for scanner.Scan() {
		line := scanner.Bytes()
		req, err := s.codec.DecodeRequest(line)
		if err != nil {
			os.Stdout.Write(s.codec.EncodeError(0, -32700, "parse error"))
			continue
		}

		resp := s.handleRequest(ctx, req, authed)
		os.Stdout.Write(resp)
	}
	return scanner.Err()
}

// ServeHTTP returns an http.Handler for the MCP server.
func (s *Server) ServeHTTP() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
		if err != nil {
			http.Error(w, "read error", 400)
			return
		}

		req, err := s.codec.DecodeRequest(body)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write(s.codec.EncodeError(0, -32700, "parse error"))
			return
		}

		// Authenticate per request via the bearer token. tools/list and
		// tools/call are refused unless the presented token matches the
		// configured one (constant-time); the handshake is always allowed.
		authHeader := r.Header.Get("Authorization")
		presented := strings.TrimPrefix(authHeader, "Bearer ")
		authed := s.checkToken(presented)

		resp := s.handleRequest(r.Context(), req, authed)
		w.Header().Set("Content-Type", "application/json")
		w.Write(resp)
	})
}

func (s *Server) handleRequest(ctx context.Context, req *JSONRPCRequest, authed bool) []byte {
	switch req.Method {
	case "tools/list", "tools/call":
		// Fail closed: never expose the global tool registry to an
		// unauthenticated caller.
		if !authed {
			return s.codec.EncodeError(req.ID, authError, "unauthorized")
		}
	}

	switch req.Method {
	case "initialize":
		result := InitializeResult{ProtocolVersion: "2024-11-05"}
		result.Capabilities.Tools = map[string]any{}
		result.ServerInfo.Name = "cyntr"
		result.ServerInfo.Version = "0.9.0"
		return s.codec.EncodeResponse(req.ID, result)

	case "notifications/initialized":
		return nil // notification, no response

	case "tools/list":
		tools := s.toolReg.List()
		var mcpTools []MCPToolDef
		for _, name := range tools {
			tool, ok := s.toolReg.Get(name)
			if !ok {
				continue
			}
			params := tool.Parameters()
			props := make(map[string]any)
			var required []string
			for pname, p := range params {
				props[pname] = map[string]any{"type": p.Type, "description": p.Description}
				if p.Required {
					required = append(required, pname)
				}
			}
			mcpTools = append(mcpTools, MCPToolDef{
				Name:        name,
				Description: tool.Description(),
				InputSchema: map[string]any{"type": "object", "properties": props, "required": required},
			})
		}
		return s.codec.EncodeResponse(req.ID, map[string]any{"tools": mcpTools})

	case "tools/call":
		paramsJSON, _ := json.Marshal(req.Params)
		var params ToolCallParams
		json.Unmarshal(paramsJSON, &params)

		input := make(map[string]string)
		for k, v := range params.Arguments {
			input[k] = fmt.Sprintf("%v", v)
		}

		// Run the tool under the authenticated caller's tenant so any
		// tenant-scoped tool enforces policy against it rather than an
		// unscoped global identity.
		if s.authTenant != "" {
			ctx = agent.WithToolCaller(ctx, s.authTenant, "mcp", "mcp")
		}

		result, err := s.toolReg.Execute(ctx, params.Name, input)
		if err != nil {
			return s.codec.EncodeResponse(req.ID, ToolCallResult{
				Content: []ContentBlock{{Type: "text", Text: err.Error()}},
				IsError: true,
			})
		}
		return s.codec.EncodeResponse(req.ID, ToolCallResult{
			Content: []ContentBlock{{Type: "text", Text: result}},
		})

	default:
		return s.codec.EncodeError(req.ID, -32601, "method not found: "+req.Method)
	}
}
