package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type mockTool struct{}

func (t *mockTool) Name() string        { return "test_tool" }
func (t *mockTool) Description() string { return "A test tool" }
func (t *mockTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{"input": {Type: "string", Description: "test input", Required: true}}
}
func (t *mockTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	return "result: " + input["input"], nil
}

func TestServerInitialize(t *testing.T) {
	reg := agent.NewToolRegistry()
	reg.Register(&mockTool{})
	srv := NewServer(reg)
	handler := srv.ServeHTTP()

	body := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp JSONRPCResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatalf("error: %s", resp.Error.Message)
	}
}

func TestServerToolsList(t *testing.T) {
	t.Setenv("CYNTR_MCP_SERVER_TOKEN", "testtok")
	reg := agent.NewToolRegistry()
	reg.Register(&mockTool{})
	srv := NewServer(reg)
	handler := srv.ServeHTTP()

	body := `{"jsonrpc":"2.0","method":"tools/list","id":2}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer testtok")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp JSONRPCResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatal(resp.Error.Message)
	}
	// Should have at least 1 tool
	resultJSON, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(resultJSON), "test_tool") {
		t.Fatalf("expected test_tool in result, got %s", resultJSON)
	}
}

func TestServerToolsCall(t *testing.T) {
	t.Setenv("CYNTR_MCP_SERVER_TOKEN", "testtok")
	reg := agent.NewToolRegistry()
	reg.Register(&mockTool{})
	srv := NewServer(reg)
	handler := srv.ServeHTTP()

	body := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"test_tool","arguments":{"input":"hello"}},"id":3}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer testtok")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp JSONRPCResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != nil {
		t.Fatal(resp.Error.Message)
	}
	resultJSON, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(resultJSON), "result: hello") {
		t.Fatalf("expected 'result: hello', got %s", resultJSON)
	}
}

func TestServerUnauthorized(t *testing.T) {
	t.Setenv("CYNTR_MCP_SERVER_TOKEN", "testtok")
	reg := agent.NewToolRegistry()
	reg.Register(&mockTool{})
	srv := NewServer(reg)
	handler := srv.ServeHTTP()

	cases := []struct {
		name string
		auth string
	}{
		{"missing token", ""},
		{"wrong token", "Bearer wrong"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, method := range []string{"tools/list", "tools/call"} {
				body := `{"jsonrpc":"2.0","method":"` + method + `","id":9}`
				req := httptest.NewRequest("POST", "/", strings.NewReader(body))
				if tc.auth != "" {
					req.Header.Set("Authorization", tc.auth)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				var resp JSONRPCResponse
				json.NewDecoder(rec.Body).Decode(&resp)
				if resp.Error == nil {
					t.Fatalf("%s: expected unauthorized error, got success", method)
				}
				if resp.Error.Message != "unauthorized" {
					t.Fatalf("%s: expected 'unauthorized', got %q", method, resp.Error.Message)
				}
			}
		})
	}
}

func TestServerUnknownMethod(t *testing.T) {
	reg := agent.NewToolRegistry()
	srv := NewServer(reg)
	handler := srv.ServeHTTP()

	body := `{"jsonrpc":"2.0","method":"unknown","id":4}`
	req := httptest.NewRequest("POST", "/", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var resp JSONRPCResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
}
