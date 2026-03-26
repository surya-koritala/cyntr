package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDatadogChannelName(t *testing.T) {
	ch := NewDatadogChannel("", "test-key")
	if ch.Name() != "datadog" {
		t.Fatalf("expected Name() = %q, got %q", "datadog", ch.Name())
	}
}

func TestDatadogChannelSendEvent(t *testing.T) {
	var receivedKey string
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("DD-API-KEY")
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	ch := NewDatadogChannel(srv.URL, "my-secret-key")

	n := Notification{
		Type:     NotifyError,
		Title:    "Deploy failed",
		Message:  "Agent deployment timed out",
		Tenant:   "acme",
		Severity: "critical",
		Agent:    "deploy-bot",
		Source:   "ci-pipeline",
	}

	err := ch.Send(context.Background(), n)
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	// Verify DD-API-KEY header
	if receivedKey != "my-secret-key" {
		t.Errorf("expected DD-API-KEY = %q, got %q", "my-secret-key", receivedKey)
	}

	// Verify title in payload
	title, ok := receivedBody["title"].(string)
	if !ok || title != "Deploy failed" {
		t.Errorf("expected title = %q, got %v", "Deploy failed", receivedBody["title"])
	}

	// Verify alert_type mapping (critical -> error)
	alertType, _ := receivedBody["alert_type"].(string)
	if alertType != "error" {
		t.Errorf("expected alert_type = %q, got %q", "error", alertType)
	}

	// Verify source_type_name
	sourceType, _ := receivedBody["source_type_name"].(string)
	if sourceType != "cyntr" {
		t.Errorf("expected source_type_name = %q, got %q", "cyntr", sourceType)
	}

	// Verify tags contain expected entries
	tagsRaw, _ := receivedBody["tags"].([]any)
	tagSet := make(map[string]bool, len(tagsRaw))
	for _, t := range tagsRaw {
		tagSet[t.(string)] = true
	}
	for _, expected := range []string{"agent:deploy-bot", "tenant:acme", "source:ci-pipeline"} {
		if !tagSet[expected] {
			t.Errorf("expected tag %q in tags, got %v", expected, tagsRaw)
		}
	}
}

func TestDatadogChannelSendMetric(t *testing.T) {
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("DD-API-KEY") == "" {
			t.Error("DD-API-KEY header missing on metric request")
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	ch := NewDatadogChannel(srv.URL, "metric-key")

	err := ch.SendMetric(context.Background(), "cyntr.agent.latency", 42.5, map[string]string{
		"agent": "summarizer",
		"env":   "prod",
	})
	if err != nil {
		t.Fatalf("SendMetric returned error: %v", err)
	}

	// Verify series structure
	seriesRaw, ok := receivedBody["series"].([]any)
	if !ok || len(seriesRaw) != 1 {
		t.Fatalf("expected series array with 1 element, got %v", receivedBody["series"])
	}

	entry, ok := seriesRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("expected series entry to be a map, got %T", seriesRaw[0])
	}

	if entry["metric"] != "cyntr.agent.latency" {
		t.Errorf("expected metric = %q, got %v", "cyntr.agent.latency", entry["metric"])
	}
	if entry["type"] != "gauge" {
		t.Errorf("expected type = %q, got %v", "gauge", entry["type"])
	}

	// Verify points structure: [[timestamp, value]]
	points, ok := entry["points"].([]any)
	if !ok || len(points) != 1 {
		t.Fatalf("expected points array with 1 element, got %v", entry["points"])
	}
	point, ok := points[0].([]any)
	if !ok || len(point) != 2 {
		t.Fatalf("expected point to be [timestamp, value], got %v", points[0])
	}
	if point[1].(float64) != 42.5 {
		t.Errorf("expected value = 42.5, got %v", point[1])
	}

	// Verify tags
	tagsRaw, _ := entry["tags"].([]any)
	tagSet := make(map[string]bool, len(tagsRaw))
	for _, tag := range tagsRaw {
		tagSet[tag.(string)] = true
	}
	if !tagSet["agent:summarizer"] {
		t.Errorf("expected tag agent:summarizer in %v", tagsRaw)
	}
	if !tagSet["env:prod"] {
		t.Errorf("expected tag env:prod in %v", tagsRaw)
	}
}
