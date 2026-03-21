package tools

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdvancedBrowserName(t *testing.T) {
	if NewAdvancedBrowserTool().Name() != "advanced_browser" {
		t.Fatal()
	}
}

func TestAdvancedBrowserGet(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body><h1>Test Page</h1><p>Content here</p></body></html>")
	}))
	defer s.Close()
	tool := NewAdvancedBrowserTool()
	r, err := tool.Execute(context.Background(), map[string]string{"action": "get", "url": s.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !containsStr(r, "Test Page") {
		t.Fatalf("got %q", r)
	}
}

func TestAdvancedBrowserExtractByTag(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html><body><h1>Title One</h1><h1>Title Two</h1><p>Not this</p></body></html>")
	}))
	defer s.Close()
	tool := NewAdvancedBrowserTool()
	r, _ := tool.Execute(context.Background(), map[string]string{"action": "extract", "url": s.URL, "selector": "h1"})
	if !containsStr(r, "Title One") {
		t.Fatalf("got %q", r)
	}
	if !containsStr(r, "Title Two") {
		t.Fatalf("got %q", r)
	}
}

func TestAdvancedBrowserExtractByClass(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<div class="price">$99</div><div class="name">Widget</div><div class="price">$149</div>`)
	}))
	defer s.Close()
	tool := NewAdvancedBrowserTool()
	r, _ := tool.Execute(context.Background(), map[string]string{"action": "extract", "url": s.URL, "selector": ".price"})
	if !containsStr(r, "$99") {
		t.Fatalf("got %q", r)
	}
}

func TestAdvancedBrowserLinks(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<a href="https://example.com">Example</a><a href="/about">About</a>`)
	}))
	defer s.Close()
	tool := NewAdvancedBrowserTool()
	r, _ := tool.Execute(context.Background(), map[string]string{"action": "links", "url": s.URL})
	if !containsStr(r, "Example") {
		t.Fatalf("got %q", r)
	}
	if !containsStr(r, "example.com") {
		t.Fatalf("got %q", r)
	}
}

func TestAdvancedBrowserFormSubmit(t *testing.T) {
	var received string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		received = r.FormValue("q")
		fmt.Fprint(w, "<html><body>Results for: "+received+"</body></html>")
	}))
	defer s.Close()
	tool := NewAdvancedBrowserTool()
	r, _ := tool.Execute(context.Background(), map[string]string{"action": "form_submit", "url": s.URL, "data": "q=cyntr+search"})
	if !containsStr(r, "cyntr search") {
		t.Fatalf("got %q", r)
	}
	if received != "cyntr search" {
		t.Fatalf("server got %q", received)
	}
}

func TestAdvancedBrowserMissingURL(t *testing.T) {
	tool := NewAdvancedBrowserTool()
	_, err := tool.Execute(context.Background(), map[string]string{"action": "get"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAdvancedBrowserUnknownAction(t *testing.T) {
	tool := NewAdvancedBrowserTool()
	_, err := tool.Execute(context.Background(), map[string]string{"action": "screenshot", "url": "http://x"})
	if err == nil {
		t.Fatal("expected error")
	}
}
