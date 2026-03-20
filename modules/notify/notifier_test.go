package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNotifierSendsToLog(t *testing.T) {
	n := NewNotifier()
	n.Send(context.Background(), Notification{
		Type: NotifyInfo, Title: "Test", Message: "Hello", Tenant: "demo",
	})
	// LogChannel prints to stdout — just verify no panic
}

func TestNotifierSlackWebhook(t *testing.T) {
	received := make(chan map[string]string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		json.NewDecoder(r.Body).Decode(&payload)
		received <- payload
		w.WriteHeader(200)
	}))
	defer server.Close()

	n := NewNotifier()
	n.AddChannel(NewSlackWebhook(server.URL))

	n.Send(context.Background(), Notification{
		Type: NotifyApproval, Title: "Approval Needed", Message: "shell_exec requested", Tenant: "finance",
	})

	select {
	case payload := <-received:
		if payload["text"] == "" {
			t.Fatal("empty text")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestNotifierChannelCount(t *testing.T) {
	n := NewNotifier()
	if n.ChannelCount() != 1 {
		t.Fatalf("expected 1 (log), got %d", n.ChannelCount())
	} // log is default
	n.AddChannel(NewSlackWebhook("http://example.com"))
	if n.ChannelCount() != 2 {
		t.Fatalf("expected 2, got %d", n.ChannelCount())
	}
}

func TestNotificationTypes(t *testing.T) {
	if NotifyApproval != "approval_needed" {
		t.Fatal()
	}
	if NotifyDenied != "policy_denied" {
		t.Fatal()
	}
	if NotifyError != "error" {
		t.Fatal()
	}
	if NotifyInfo != "info" {
		t.Fatal()
	}
}

func TestLogChannelName(t *testing.T) {
	if (&LogChannel{}).Name() != "log" {
		t.Fatal()
	}
}

func TestSlackWebhookName(t *testing.T) {
	if NewSlackWebhook("").Name() != "slack_webhook" {
		t.Fatal()
	}
}
