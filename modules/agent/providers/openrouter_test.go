package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

func TestOpenRouterProviderName(t *testing.T) {
	p := NewOpenRouter("key", "anthropic/claude-3.5-sonnet", "")
	if p.Name() != "openrouter" {
		t.Fatalf("expected openrouter, got %q", p.Name())
	}
}

func TestOpenRouterProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatal("missing auth")
		}
		if r.Header.Get("HTTP-Referer") != "https://cyntr.dev" {
			t.Fatal("missing HTTP-Referer header")
		}
		if r.Header.Get("X-Title") != "Cyntr" {
			t.Fatal("missing X-Title header")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Hello from OpenRouter!"}}]}`)
	}))
	defer server.Close()

	p := NewOpenRouter("test-key", "anthropic/claude-3.5-sonnet", server.URL)
	resp, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content != "Hello from OpenRouter!" {
		t.Fatalf("got %q", resp.Content)
	}
}

func TestOpenRouterProviderToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"London\"}"}}]}}]}`)
	}))
	defer server.Close()

	p := NewOpenRouter("key", "model", server.URL)
	resp, _ := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Weather?"}}, nil)
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("got %q", resp.ToolCalls[0].Name)
	}
}

func TestOpenRouterProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"invalid key"}}`)
	}))
	defer server.Close()

	p := NewOpenRouter("bad", "model", server.URL)
	_, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Hi"}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenRouterProviderEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[]}`)
	}))
	defer server.Close()

	p := NewOpenRouter("key", "model", server.URL)
	resp, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content != "" {
		t.Fatalf("expected empty, got %q", resp.Content)
	}
}
