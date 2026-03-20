package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPToolName(t *testing.T) {
	tool := NewHTTPTool()
	if tool.Name() != "http_request" {
		t.Fatalf("expected http_request, got %q", tool.Name())
	}
}

func TestHTTPToolGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello from server")
	}))
	defer server.Close()

	tool := NewHTTPTool()
	result, err := tool.Execute(context.Background(), map[string]string{"url": server.URL})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "hello from server") {
		t.Fatalf("expected server response, got %q", result)
	}
	if !strings.Contains(result, "Status: 200") {
		t.Fatalf("expected status 200, got %q", result)
	}
}

func TestHTTPToolPost(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 1024)
		n, _ := r.Body.Read(b)
		receivedBody = string(b[:n])
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	tool := NewHTTPTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"url": server.URL, "method": "POST", "body": "test data",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if receivedBody != "test data" {
		t.Fatalf("expected 'test data', got %q", receivedBody)
	}
}

func TestHTTPToolHeaders(t *testing.T) {
	var receivedHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Custom")
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	tool := NewHTTPTool()
	tool.Execute(context.Background(), map[string]string{
		"url": server.URL, "headers": `{"X-Custom":"test-value"}`,
	})
	if receivedHeader != "test-value" {
		t.Fatalf("expected header, got %q", receivedHeader)
	}
}

func TestHTTPToolMissingURL(t *testing.T) {
	tool := NewHTTPTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestHTTPToolBadURL(t *testing.T) {
	tool := NewHTTPTool()
	_, err := tool.Execute(context.Background(), map[string]string{"url": "http://127.0.0.1:1"})
	if err == nil {
		t.Fatal("expected error for unreachable URL")
	}
}
