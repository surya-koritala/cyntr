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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "test-key" {
			t.Fatal("missing api key")
		}
		if r.URL.Query().Get("cx") != "test-cx" {
			t.Fatal("missing cx")
		}
		if r.URL.Query().Get("q") != "golang testing" {
			t.Fatal("missing query")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]string{
				{"title": "Go Testing", "link": "https://go.dev/doc/testing", "snippet": "How to write tests in Go"},
				{"title": "Testing Package", "link": "https://pkg.go.dev/testing", "snippet": "Package testing provides support"},
			},
		})
	}))
	defer server.Close()

	tool := NewWebSearchTool()
	tool.SetAPIURL(server.URL)

	result, err := tool.Execute(context.Background(), map[string]string{
		"query": "golang testing", "api_key": "test-key", "cx": "test-cx",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "Go Testing") {
		t.Fatalf("expected title, got %q", result)
	}
	if !strings.Contains(result, "https://go.dev/doc/testing") {
		t.Fatalf("expected URL, got %q", result)
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
	var receivedNum string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedNum = r.URL.Query().Get("num")
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer server.Close()

	tool := NewWebSearchTool()
	tool.SetAPIURL(server.URL)
	tool.Execute(context.Background(), map[string]string{
		"query": "test", "api_key": "key", "cx": "cx", "num_results": "3",
	})
	if receivedNum != "3" {
		t.Fatalf("expected num=3, got %q", receivedNum)
	}
}
