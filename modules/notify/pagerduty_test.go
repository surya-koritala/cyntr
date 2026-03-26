package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPagerDutyChannelName(t *testing.T) {
	ch := NewPagerDutyChannel("", "test-key")
	if ch.Name() != "pagerduty" {
		t.Fatalf("expected Name() = %q, got %q", "pagerduty", ch.Name())
	}
}

func TestPagerDutyChannelSend(t *testing.T) {
	var received pagerDutyEvent

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", ct)
		}

		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	ch := NewPagerDutyChannel(srv.URL, "routing-key-123")

	n := Notification{
		Type:     NotifyError,
		Title:    "Disk Full",
		Message:  "Root partition is at 98%",
		Tenant:   "acme",
		Severity: "critical",
		Agent:    "infra-agent",
		Source:   "disk-monitor",
		Fields:   map[string]string{"host": "web-01"},
	}

	err := ch.Send(context.Background(), n)
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	if received.RoutingKey != "routing-key-123" {
		t.Errorf("expected routing_key %q, got %q", "routing-key-123", received.RoutingKey)
	}
	if received.EventAction != "trigger" {
		t.Errorf("expected event_action %q, got %q", "trigger", received.EventAction)
	}
	if received.DedupKey != "cyntr-acme-infra-agent-disk-monitor" {
		t.Errorf("expected dedup_key %q, got %q", "cyntr-acme-infra-agent-disk-monitor", received.DedupKey)
	}
	if received.Payload == nil {
		t.Fatal("expected payload, got nil")
	}
	if received.Payload.Severity != "critical" {
		t.Errorf("expected severity %q, got %q", "critical", received.Payload.Severity)
	}
	if received.Payload.Component != "infra-agent" {
		t.Errorf("expected component %q, got %q", "infra-agent", received.Payload.Component)
	}
	if received.Payload.Group != "acme" {
		t.Errorf("expected group %q, got %q", "acme", received.Payload.Group)
	}
	expectedSummary := "Disk Full: Root partition is at 98%"
	if received.Payload.Summary != expectedSummary {
		t.Errorf("expected summary %q, got %q", expectedSummary, received.Payload.Summary)
	}
	if received.Payload.CustomDetails["host"] != "web-01" {
		t.Errorf("expected custom_details[host] = %q, got %q", "web-01", received.Payload.CustomDetails["host"])
	}
}

func TestPagerDutyChannelResolve(t *testing.T) {
	var received pagerDutyEvent

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	ch := NewPagerDutyChannel(srv.URL, "routing-key-456")

	n := Notification{
		Type:     NotifyInfo,
		Title:    "Disk OK",
		Message:  "Root partition back to normal",
		Tenant:   "acme",
		Severity: "info",
		Agent:    "infra-agent",
		Source:   "disk-monitor",
	}

	err := ch.Send(context.Background(), n)
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	if received.EventAction != "resolve" {
		t.Errorf("expected event_action %q, got %q", "resolve", received.EventAction)
	}
	if received.DedupKey != "cyntr-acme-infra-agent-disk-monitor" {
		t.Errorf("expected dedup_key %q, got %q", "cyntr-acme-infra-agent-disk-monitor", received.DedupKey)
	}
}
