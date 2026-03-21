package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendNotificationToolName(t *testing.T) {
	if NewSendNotificationTool().Name() != "send_notification" {
		t.Fatal("wrong name")
	}
}

func TestSendNotificationSuccess(t *testing.T) {
	var received map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(200)
	}))
	defer server.Close()

	tool := NewSendNotificationTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"webhook_url": server.URL, "title": "Deploy Complete", "message": "v1.2.3 deployed", "severity": "info",
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !strings.Contains(result, "Notification sent") {
		t.Fatalf("expected success, got %q", result)
	}
	if !strings.Contains(received["text"], "Deploy Complete") {
		t.Fatalf("expected title in payload, got %q", received["text"])
	}
}

func TestSendNotificationCritical(t *testing.T) {
	var received map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(200)
	}))
	defer server.Close()

	tool := NewSendNotificationTool()
	tool.Execute(context.Background(), map[string]string{
		"webhook_url": server.URL, "title": "Alert", "message": "CPU 99%", "severity": "critical",
	})
	if received["text"] == "" {
		t.Fatal("expected payload")
	}
}

func TestSendNotificationWebhookError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	tool := NewSendNotificationTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"webhook_url": server.URL, "title": "Test", "message": "Test",
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestSendNotificationMissingParams(t *testing.T) {
	tool := NewSendNotificationTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing params")
	}
}

func TestSendNotificationParams(t *testing.T) {
	tool := NewSendNotificationTool()
	params := tool.Parameters()
	if !params["webhook_url"].Required || !params["title"].Required || !params["message"].Required {
		t.Fatal("webhook_url, title, message should be required")
	}
	if params["severity"].Required {
		t.Fatal("severity should not be required")
	}
}
