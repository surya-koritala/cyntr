package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

func TestAzureOpenAIProviderName(t *testing.T) {
	p := NewAzureOpenAI("key", "https://myresource.openai.azure.com", "gpt-4", "")
	if p.Name() != "azure-openai" {
		t.Fatalf("expected azure-openai, got %q", p.Name())
	}
}

func TestAzureOpenAIChatURL(t *testing.T) {
	p := NewAzureOpenAI("key", "https://myresource.openai.azure.com", "gpt-4o", "2024-08-01-preview")
	url := p.chatURL()
	expected := "https://myresource.openai.azure.com/openai/deployments/gpt-4o/chat/completions?api-version=2024-08-01-preview"
	if url != expected {
		t.Fatalf("expected %q, got %q", expected, url)
	}
}

func TestAzureOpenAIDefaultAPIVersion(t *testing.T) {
	p := NewAzureOpenAI("key", "https://example.com", "deploy", "")
	if p.apiVersion != "2024-08-01-preview" {
		t.Fatalf("expected default api version, got %q", p.apiVersion)
	}
}

func TestAzureOpenAIChat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Azure-style auth header
		if r.Header.Get("api-key") != "test-key" {
			t.Fatal("missing api-key header")
		}
		// Should NOT have Bearer auth
		if r.Header.Get("Authorization") != "" {
			t.Fatal("should not have Authorization header")
		}
		// Verify URL contains deployment path
		if !strings.Contains(r.URL.Path, "/openai/deployments/my-gpt4/chat/completions") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		// Verify api-version query param
		if r.URL.Query().Get("api-version") != "2024-08-01-preview" {
			t.Fatalf("unexpected api-version: %s", r.URL.Query().Get("api-version"))
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Hello from Azure!"}}]}`)
	}))
	defer server.Close()

	p := NewAzureOpenAI("test-key", server.URL, "my-gpt4", "2024-08-01-preview")
	resp, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content != "Hello from Azure!" {
		t.Fatalf("got %q", resp.Content)
	}
}

func TestAzureOpenAIToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Seattle\"}"}}]}}]}`)
	}))
	defer server.Close()

	p := NewAzureOpenAI("key", server.URL, "gpt-4o", "")
	resp, _ := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Weather?"}}, nil)
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("got %q", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].Input["city"] != "Seattle" {
		t.Fatalf("got %q", resp.ToolCalls[0].Input["city"])
	}
}

func TestAzureOpenAIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"Access denied due to invalid subscription key"}}`)
	}))
	defer server.Close()

	p := NewAzureOpenAI("bad-key", server.URL, "gpt-4", "")
	_, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Hi"}}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 in error, got %v", err)
	}
}

func TestAzureOpenAIEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[]}`)
	}))
	defer server.Close()

	p := NewAzureOpenAI("key", server.URL, "gpt-4", "")
	resp, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "Hi"}}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content != "" {
		t.Fatalf("expected empty, got %q", resp.Content)
	}
}

func TestAzureOpenAITrailingSlash(t *testing.T) {
	p := NewAzureOpenAI("key", "https://example.com/", "deploy", "")
	url := p.chatURL()
	// Should not have double slash between endpoint and path
	if strings.Contains(url, ".com//openai") {
		t.Fatalf("double slash in URL: %s", url)
	}
}
