package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGenericWebhookSend(t *testing.T) {
	var receivedBody map[string]string
	var receivedHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := NewGenericWebhookChannel("test-hook", srv.URL, map[string]string{
		"X-Custom": "hello",
	})

	n := Notification{
		Type:     NotifyInfo,
		Title:    "deploy complete",
		Message:  "v2.1.0 rolled out",
		Tenant:   "acme",
		Severity: "info",
		Agent:    "deploy-bot",
		Source:   "ci-pipeline",
	}

	err := ch.Send(context.Background(), n)
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	if receivedBody["title"] != "deploy complete" {
		t.Errorf("expected title 'deploy complete', got %q", receivedBody["title"])
	}

	if receivedHeaders.Get("X-Custom") != "hello" {
		t.Errorf("expected X-Custom header 'hello', got %q", receivedHeaders.Get("X-Custom"))
	}

	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %q", receivedHeaders.Get("Content-Type"))
	}
}

func TestGenericWebhookRetry(t *testing.T) {
	var attempts atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := attempts.Add(1)
		if count < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := NewGenericWebhookChannel("retry-hook", srv.URL, nil)
	ch.MaxRetries = 3
	ch.RetryDelayMs = 10

	err := ch.Send(context.Background(), Notification{Title: "retry-test"})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}

	if got := attempts.Load(); got != 3 {
		t.Errorf("expected 3 attempts, got %d", got)
	}
}

func TestGenericWebhookName(t *testing.T) {
	ch := NewGenericWebhookChannel("my-webhook", "http://example.com/hook", nil)
	if ch.Name() != "my-webhook" {
		t.Errorf("expected name 'my-webhook', got %q", ch.Name())
	}
}

func TestGenericWebhookMaxRetriesExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ch := NewGenericWebhookChannel("fail-hook", srv.URL, nil)
	ch.MaxRetries = 2
	ch.RetryDelayMs = 10

	err := ch.Send(context.Background(), Notification{Title: "will-fail"})
	if err == nil {
		t.Fatal("expected error when all retries exhausted, got nil")
	}

	if !strings.Contains(err.Error(), "attempts exhausted") {
		t.Errorf("expected 'attempts exhausted' in error, got: %v", err)
	}
}
