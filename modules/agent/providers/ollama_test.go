package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

func TestOllamaProviderName(t *testing.T) {
	// Name() returns the model so multiple Ollama models register under
	// distinct names (consistent with Azure/OpenAI-compatible providers).
	p := NewOllama("llama3", "")
	if p.Name() != "llama3" {
		t.Fatalf("expected llama3, got %q", p.Name())
	}
}

func TestOllamaProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"message":{"role":"assistant","content":"Hello from Ollama!"}}`)
	}))
	defer server.Close()

	p := NewOllama("llama3", server.URL)
	resp, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content != "Hello from Ollama!" {
		t.Fatalf("got %q", resp.Content)
	}
}

func TestOllamaProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `{"error":"model not found"}`)
	}))
	defer server.Close()

	p := NewOllama("nonexistent", server.URL)
	_, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Hi"}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
