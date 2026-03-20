package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

func TestGeminiProviderName(t *testing.T) {
	p := NewGemini("key", "gemini-pro", "")
	if p.Name() != "gemini" {
		t.Fatalf("got %q", p.Name())
	}
}

func TestGeminiProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "test-key" {
			t.Fatal("missing key")
		}
		fmt.Fprint(w, `{"candidates":[{"content":{"parts":[{"text":"Hello from Gemini!"}]}}]}`)
	}))
	defer server.Close()

	p := NewGemini("test-key", "gemini-pro", server.URL)
	resp, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content != "Hello from Gemini!" {
		t.Fatalf("got %q", resp.Content)
	}
}

func TestGeminiProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":"bad request"}`)
	}))
	defer server.Close()

	p := NewGemini("key", "gemini-pro", server.URL)
	_, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Hi"}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
