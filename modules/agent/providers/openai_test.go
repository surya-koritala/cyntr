package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

func TestOpenAIProviderName(t *testing.T) {
	p := NewOpenAI("key", "gpt-4", "")
	if p.Name() != "gpt" {
		t.Fatalf("expected gpt, got %q", p.Name())
	}
}

func TestOpenAIProviderChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatal("missing auth")
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Hello from GPT!"}}]}`)
	}))
	defer server.Close()

	p := NewOpenAI("test-key", "gpt-4", server.URL)
	resp, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content != "Hello from GPT!" {
		t.Fatalf("got %q", resp.Content)
	}
}

func TestOpenAIProviderToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"London\"}"}}]}}]}`)
	}))
	defer server.Close()

	p := NewOpenAI("key", "gpt-4", server.URL)
	resp, _ := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Weather?"}}, nil)
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("got %q", resp.ToolCalls[0].Name)
	}
}

func TestOpenAIProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"invalid key"}}`)
	}))
	defer server.Close()

	p := NewOpenAI("bad", "gpt-4", server.URL)
	_, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Hi"}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
