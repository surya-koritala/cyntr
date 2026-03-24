package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// Server exposes Cyntr tools as an MCP server.
type Server struct {
	toolReg *agent.ToolRegistry
	codec   *Codec
}

func NewServer(toolReg *agent.ToolRegistry) *Server {
	return &Server{toolReg: toolReg, codec: NewCodec()}
}

// ServeStdio runs the MCP server on stdin/stdout.
func (s *Server) ServeStdio(ctx context.Context) error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		req, err := s.codec.DecodeRequest(line)
		if err != nil {
			os.Stdout.Write(s.codec.EncodeError(0, -32700, "parse error"))
			continue
		}

		resp := s.handleRequest(ctx, req)
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

		resp := s.handleRequest(r.Context(), req)
		w.Header().Set("Content-Type", "application/json")
		w.Write(resp)
	})
}

func (s *Server) handleRequest(ctx context.Context, req *JSONRPCRequest) []byte {
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
