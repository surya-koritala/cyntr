package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowserToolName(t *testing.T) {
	if NewBrowserTool().Name() != "browse_web" {
		t.Fatal("wrong name")
	}
}

func TestBrowserToolFetchesPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body><h1>Hello World</h1><p>This is a test page.</p></body></html>")
	}))
	defer server.Close()

	tool := NewBrowserTool()
	result, err := tool.Execute(context.Background(), map[string]string{"url": server.URL})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsStr(result, "Hello World") {
		t.Fatalf("missing content: %q", result)
	}
	if !containsStr(result, "test page") {
		t.Fatalf("missing paragraph: %q", result)
	}
	if !containsStr(result, "Status: 200") {
		t.Fatalf("missing status: %q", result)
	}
}

func TestBrowserToolStripsScripts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body><script>alert('xss')</script><p>Safe content</p></body></html>")
	}))
	defer server.Close()

	tool := NewBrowserTool()
	result, _ := tool.Execute(context.Background(), map[string]string{"url": server.URL})
	if containsStr(result, "alert") {
		t.Fatal("script should be stripped")
	}
	if !containsStr(result, "Safe content") {
		t.Fatal("missing safe content")
	}
}

func TestBrowserToolMissingURL(t *testing.T) {
	tool := NewBrowserTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBrowserToolBadURL(t *testing.T) {
	tool := NewBrowserTool()
	_, err := tool.Execute(context.Background(), map[string]string{"url": "http://127.0.0.1:1"})
	if err == nil {
		t.Fatal("expected error for unreachable")
	}
}

func TestExtractText(t *testing.T) {
	html := "<html><head><style>body{}</style></head><body><h1>Title</h1><p>Hello &amp; world</p></body></html>"
	text := extractText(html)
	if !containsStr(text, "Title") {
		t.Fatalf("missing title: %q", text)
	}
	if !containsStr(text, "Hello & world") {
		t.Fatalf("missing decoded entity: %q", text)
	}
	if containsStr(text, "body{}") {
		t.Fatal("style should be removed")
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && findStr(s, sub)
}
func findStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
