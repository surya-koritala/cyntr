package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

func mockAnthropicServer(response string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request format
		if r.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		if r.Header.Get("X-API-Key") == "" {
			http.Error(w, "missing API key", 401)
			return
		}
		if r.Header.Get("Anthropic-Version") == "" {
			http.Error(w, "missing version", 400)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, response)
	}))
}

func mockAnthropicStreamServer(events []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", 500)
			return
		}

		for _, event := range events {
			fmt.Fprint(w, event)
			flusher.Flush()
		}
	}))
}

func TestAnthropicProviderName(t *testing.T) {
	p := NewAnthropic("test-key", "claude-sonnet-4-20250514", "")
	if p.Name() != "claude" {
		t.Fatalf("expected 'claude', got %q", p.Name())
	}
}

func TestAnthropicProviderChat(t *testing.T) {
	server := mockAnthropicServer(`{
		"id": "msg_01",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello! How can I help?"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 15}
	}`)
	defer server.Close()

	p := NewAnthropic("test-key", "claude-sonnet-4-20250514", server.URL)

	resp, err := p.Chat(context.Background(), []agent.Message{
		{Role: agent.RoleUser, Content: "Hello"},
	}, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Role != agent.RoleAssistant {
		t.Fatalf("expected assistant, got %s", resp.Role)
	}
	if resp.Content != "Hello! How can I help?" {
		t.Fatalf("expected greeting, got %q", resp.Content)
	}
}

func TestAnthropicProviderChatWithToolUse(t *testing.T) {
	server := mockAnthropicServer(`{
		"id": "msg_02",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Let me check that."},
			{"type": "tool_use", "id": "toolu_01", "name": "get_weather", "input": {"city": "London"}}
		],
		"stop_reason": "tool_use"
	}`)
	defer server.Close()

	p := NewAnthropic("test-key", "claude-sonnet-4-20250514", server.URL)

	resp, err := p.Chat(context.Background(), []agent.Message{
		{Role: agent.RoleUser, Content: "What's the weather?"},
	}, []agent.ToolDef{{Name: "get_weather", Description: "Get weather"}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("expected get_weather, got %q", resp.ToolCalls[0].Name)
	}
	if resp.ToolCalls[0].ID != "toolu_01" {
		t.Fatalf("expected toolu_01, got %q", resp.ToolCalls[0].ID)
	}
}

func TestAnthropicProviderChatStream(t *testing.T) {
	server := mockAnthropicStreamServer([]string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"role\":\"assistant\"}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	})
	defer server.Close()

	p := NewAnthropic("test-key", "claude-sonnet-4-20250514", server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := p.ChatStream(ctx, []agent.Message{
		{Role: agent.RoleUser, Content: "Hi"},
	}, nil)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	var fullText string
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("stream error: %v", chunk.Error)
		}
		if chunk.Type == "text" {
			fullText += chunk.Text
		}
	}

	if fullText != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", fullText)
	}
}

func TestAnthropicProviderStreamToolUse(t *testing.T) {
	server := mockAnthropicStreamServer([]string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_01\",\"role\":\"assistant\"}}\n\n",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_01\",\"name\":\"get_weather\",\"input\":{}}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"city\\\": \\\"London\\\"}\"}}\n\n",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	})
	defer server.Close()

	p := NewAnthropic("test-key", "claude-sonnet-4-20250514", server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := p.ChatStream(ctx, []agent.Message{
		{Role: agent.RoleUser, Content: "Weather?"},
	}, []agent.ToolDef{{Name: "get_weather"}})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	var gotToolStart bool
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("error: %v", chunk.Error)
		}
		if chunk.Type == "tool_use_start" {
			gotToolStart = true
			if chunk.ToolCall == nil || chunk.ToolCall.Name != "get_weather" {
				t.Fatalf("expected get_weather tool call")
			}
		}
	}
	if !gotToolStart {
		t.Fatal("expected tool_use_start chunk")
	}
}

func TestAnthropicProviderBadAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	}))
	defer server.Close()

	p := NewAnthropic("bad-key", "claude-sonnet-4-20250514", server.URL)

	_, err := p.Chat(context.Background(), []agent.Message{
		{Role: agent.RoleUser, Content: "Hi"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for bad API key")
	}
}

func TestAnthropicProviderImplementsStreaming(t *testing.T) {
	var _ agent.StreamingProvider = (*Anthropic)(nil)
}
