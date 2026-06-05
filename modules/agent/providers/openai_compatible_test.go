package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

func TestOpenAICompatibleChat(t *testing.T) {
	var gotModel, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "wrong path", 404)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"choices":[{"message":{"role":"assistant","content":"hi there",
				"tool_calls":[{"id":"t1","type":"function","function":{"name":"echo","arguments":"{\"text\":\"x\"}"}}]}}],
			"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
		}`))
	}))
	defer srv.Close()

	p := NewOpenAICompatible("glm", "sekret", "glm-4", srv.URL)
	if p.Name() != "glm" {
		t.Fatalf("name = %q, want glm", p.Name())
	}

	msg, err := p.Chat(context.Background(),
		[]agent.Message{{Role: agent.RoleUser, Content: "hi"}},
		[]agent.ToolDef{{Name: "echo", Parameters: map[string]agent.ToolParam{"text": {Type: "string"}}}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	if gotAuth != "Bearer sekret" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if gotModel != "glm-4" {
		t.Fatalf("model in request = %q, want glm-4", gotModel)
	}
	if msg.Content != "hi there" {
		t.Fatalf("content = %q", msg.Content)
	}
	if msg.InputTokens != 10 || msg.OutputTokens != 5 {
		t.Fatalf("usage not reported: in=%d out=%d", msg.InputTokens, msg.OutputTokens)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Name != "echo" || msg.ToolCalls[0].Input["text"] != "x" {
		t.Fatalf("tool call wrong: %+v", msg.ToolCalls)
	}
}

func TestOpenAICompatibleErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad model"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	p := NewOpenAICompatible("kimi", "k", "unknown-model", srv.URL)
	if _, err := p.Chat(context.Background(), []agent.Message{{Role: agent.RoleUser, Content: "hi"}}, nil); err == nil {
		t.Fatal("a non-200 status should surface a clean error")
	}
}

func TestOpenAICompatibleDefaultName(t *testing.T) {
	p := NewOpenAICompatible("", "k", "m", "http://x")
	if p.Name() != "openai-compatible" {
		t.Fatalf("empty name should default, got %q", p.Name())
	}
}
