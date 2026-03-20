package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestRespondSuccess(t *testing.T) {
	w := httptest.NewRecorder()
	Respond(w, 200, map[string]string{"name": "test"})

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)

	if env.Error != nil {
		t.Fatalf("expected no error, got %v", env.Error)
	}
	if env.Meta.RequestID == "" {
		t.Fatal("expected request ID")
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", env.Data)
	}
	if data["name"] != "test" {
		t.Fatalf("expected test, got %v", data["name"])
	}
}

func TestRespondError(t *testing.T) {
	w := httptest.NewRecorder()
	RespondError(w, 403, "POLICY_DENIED", "action denied by policy")

	if w.Code != 403 {
		t.Fatalf("expected 403, got %d", w.Code)
	}

	var env Envelope
	json.NewDecoder(w.Body).Decode(&env)

	if env.Data != nil {
		t.Fatalf("expected nil data, got %v", env.Data)
	}
	if env.Error == nil {
		t.Fatal("expected error")
	}
	if env.Error.Code != "POLICY_DENIED" {
		t.Fatalf("expected POLICY_DENIED, got %q", env.Error.Code)
	}
}
