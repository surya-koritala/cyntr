package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientConnectHTTP(t *testing.T) {
	reqCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		var req JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)

		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			json.NewEncoder(w).Encode(JSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Result: map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "test", "version": "1.0"},
				},
			})
		case "tools/list":
			json.NewEncoder(w).Encode(JSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Result: map[string]any{
					"tools": []map[string]any{
						{"name": "test_tool", "description": "A test tool", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
					},
				},
			})
		default:
			json.NewEncoder(w).Encode(JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
		}
	}))
	defer server.Close()

	client := NewClient(ServerConfig{Name: "test", Transport: "http", URL: server.URL})
	err := client.Connect(context.Background())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !client.IsConnected() {
		t.Fatal("expected connected")
	}
	if len(client.Tools()) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(client.Tools()))
	}
	if client.Tools()[0].Name != "test_tool" {
		t.Fatalf("expected test_tool, got %q", client.Tools()[0].Name)
	}
}

func TestClientCallToolHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")

		if req.Method == "initialize" || req.Method == "tools/list" {
			json.NewEncoder(w).Encode(JSONRPCResponse{
				JSONRPC: "2.0", ID: req.ID,
				Result: map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{},
					"serverInfo":      map[string]any{"name": "test", "version": "1.0"},
					"tools":           []any{},
				},
			})
			return
		}

		// tools/call response
		json.NewEncoder(w).Encode(JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "tool result here"},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(ServerConfig{Name: "test", Transport: "http", URL: server.URL})
	client.Connect(context.Background())

	result, err := client.CallTool(context.Background(), "test_tool", map[string]any{"input": "hello"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("expected content")
	}
	if result.Content[0].Text != "tool result here" {
		t.Fatalf("expected 'tool result here', got %q", result.Content[0].Text)
	}
}
