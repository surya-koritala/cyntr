package proxy

import "testing"

func TestIntentString(t *testing.T) {
	i := Intent{
		Action:   "model_call",
		Provider: "anthropic",
		Model:    "claude-sonnet-4-20250514",
		Tool:     "",
	}
	if i.Action != "model_call" {
		t.Fatalf("expected model_call, got %q", i.Action)
	}
}

func TestExternalAgentKey(t *testing.T) {
	ea := ExternalAgent{
		Name:     "marketing-openclaw",
		Tenant:   "marketing",
		Type:     "openclaw",
		Endpoint: "http://localhost:18789",
	}
	if ea.Key() != "marketing/marketing-openclaw" {
		t.Fatalf("expected marketing/marketing-openclaw, got %q", ea.Key())
	}
}
