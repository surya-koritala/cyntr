package parsers

import (
	"net/http"
	"strings"
	"testing"
)

func TestOpenAIParserDetectsModelCall(t *testing.T) {
	body := `{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}`
	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-test")

	p := &OpenAIParser{}
	intent, err := p.Parse(req)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if intent.Action != "model_call" {
		t.Fatalf("expected model_call, got %q", intent.Action)
	}
	if intent.Provider != "openai" {
		t.Fatalf("expected openai, got %q", intent.Provider)
	}
	if intent.Model != "gpt-4" {
		t.Fatalf("expected gpt-4, got %q", intent.Model)
	}
}

func TestOpenAIParserDetectsToolUse(t *testing.T) {
	body := `{"model":"gpt-4","messages":[],"tools":[{"type":"function","function":{"name":"get_weather"}}]}`
	req, _ := http.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	p := &OpenAIParser{}
	intent, _ := p.Parse(req)
	if intent.Tool != "get_weather" {
		t.Fatalf("expected get_weather, got %q", intent.Tool)
	}
}

func TestOpenAIParserMatches(t *testing.T) {
	req, _ := http.NewRequest("POST", "/v1/chat/completions", nil)
	p := &OpenAIParser{}
	if !p.Matches(req) {
		t.Fatal("expected match")
	}
}

func TestOpenAIParserNoMatch(t *testing.T) {
	req, _ := http.NewRequest("GET", "/health", nil)
	p := &OpenAIParser{}
	if p.Matches(req) {
		t.Fatal("expected no match")
	}
}
