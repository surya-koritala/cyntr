package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSearchToolName(t *testing.T) {
	if NewWebSearchTool().Name() != "web_search" {
		t.Fatal("unexpected name")
	}
}

func TestWebSearchToolExecute(t *testing.T) {
	// The tool posts to Firecrawl's POST /v1/search with a JSON body and
	// parses {success, data:[{url,title,description,markdown}]}.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["query"] != "golang testing" {
			t.Errorf("query in body = %v", body["query"])
		}
		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []map[string]string{
				{"title": "Go Testing", "url": "https://go.dev/doc/testing", "description": "How to write tests in Go"},
			},
		})
	}))
	defer server.Close()

	tool := NewWebSearchTool()
	tool.SetAPIURL(server.URL)

	result, err := tool.Execute(context.Background(), map[string]string{"query": "golang testing"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "Go Testing") || !strings.Contains(result, "https://go.dev/doc/testing") {
		t.Fatalf("expected title+url, got %q", result)
	}
}

func TestWebSearchToolNoResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer server.Close()

	tool := NewWebSearchTool()
	tool.SetAPIURL(server.URL)

	result, err := tool.Execute(context.Background(), map[string]string{
		"query": "xyznoexist", "api_key": "key", "cx": "cx",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result != "No results found." {
		t.Fatalf("expected no results, got %q", result)
	}
}

func TestWebSearchToolMissingParams(t *testing.T) {
	tool := NewWebSearchTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing params")
	}
}

func TestWebSearchToolAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"error":"forbidden"}`)
	}))
	defer server.Close()

	tool := NewWebSearchTool()
	tool.SetAPIURL(server.URL)
	_, err := tool.Execute(context.Background(), map[string]string{
		"query": "test", "api_key": "bad", "cx": "cx",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWebSearchToolNumResults(t *testing.T) {
	var receivedLimit float64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		receivedLimit, _ = body["limit"].(float64)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
	}))
	defer server.Close()

	tool := NewWebSearchTool()
	tool.SetAPIURL(server.URL)
	tool.Execute(context.Background(), map[string]string{"query": "test", "num_results": "3"})
	if receivedLimit != 3 {
		t.Fatalf("expected limit=3 in body, got %v", receivedLimit)
	}
}
