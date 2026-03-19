package parsers

import (
	"net/http"
	"strings"
	"testing"
)

func TestAnthropicParserDetectsModelCall(t *testing.T) {
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Hello"}]}`
	req, _ := http.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "sk-test")

	p := &AnthropicParser{}
	intent, err := p.Parse(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if intent.Action != "model_call" {
		t.Fatalf("expected model_call, got %q", intent.Action)
	}
	if intent.Provider != "anthropic" {
		t.Fatalf("expected anthropic, got %q", intent.Provider)
	}
	if intent.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("expected claude-sonnet-4-20250514, got %q", intent.Model)
	}
}

func TestAnthropicParserDetectsToolUse(t *testing.T) {
	body := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"Run ls"}],"tools":[{"name":"shell_exec"}]}`
	req, _ := http.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	p := &AnthropicParser{}
	intent, _ := p.Parse(req)
	if intent.Tool != "shell_exec" {
		t.Fatalf("expected shell_exec tool, got %q", intent.Tool)
	}
}

func TestAnthropicParserNoMatch(t *testing.T) {
	req, _ := http.NewRequest("GET", "/health", nil)
	p := &AnthropicParser{}
	ok := p.Matches(req)
	if ok {
		t.Fatal("expected no match for GET /health")
	}
}

func TestAnthropicParserMatches(t *testing.T) {
	req, _ := http.NewRequest("POST", "/v1/messages", nil)
	req.Header.Set("X-API-Key", "sk-test")
	p := &AnthropicParser{}
	if !p.Matches(req) {
		t.Fatal("expected match for POST /v1/messages with X-API-Key")
	}
}
