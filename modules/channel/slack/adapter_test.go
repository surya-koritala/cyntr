package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

func TestSlackAdapterImplementsInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestSlackAdapterName(t *testing.T) {
	a := New("127.0.0.1:0", "xoxb-test", "marketing", "assistant")
	if a.Name() != "slack" {
		t.Fatalf("expected slack, got %q", a.Name())
	}
}

func TestSlackAdapterURLVerification(t *testing.T) {
	a := New("127.0.0.1:0", "xoxb-test", "marketing", "assistant")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"url_verification","challenge":"test-challenge-123"}`
	resp, err := http.Post("http://"+a.Addr()+"/slack/events", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["challenge"] != "test-challenge-123" {
		t.Fatalf("expected challenge, got %v", result)
	}
}

func TestSlackAdapterReceivesMessage(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)

	// Mock Slack API for chat.postMessage
	slackAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer slackAPI.Close()

	a := New("127.0.0.1:0", "xoxb-test", "marketing", "assistant")
	a.SetSlackAPI(slackAPI.URL)

	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "Agent reply!", nil
	})
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"event_callback","event":{"type":"message","user":"U123","text":"Hello agent","channel":"C456"}}`
	resp, _ := http.Post("http://"+a.Addr()+"/slack/events", "application/json", strings.NewReader(body))
	resp.Body.Close()

	select {
	case msg := <-received:
		if msg.Text != "Hello agent" {
			t.Fatalf("expected message, got %q", msg.Text)
		}
		if msg.UserID != "U123" {
			t.Fatalf("expected U123, got %q", msg.UserID)
		}
		if msg.ChannelID != "C456" {
			t.Fatalf("expected C456, got %q", msg.ChannelID)
		}
		if msg.Tenant != "marketing" {
			t.Fatalf("expected marketing, got %q", msg.Tenant)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestSlackAdapterSkipsBotMessages(t *testing.T) {
	handlerCalled := false
	a := New("127.0.0.1:0", "xoxb-test", "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		handlerCalled = true
		return "", nil
	})
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"event_callback","event":{"type":"message","user":"U123","text":"bot msg","channel":"C456","bot_id":"B789"}}`
	resp, _ := http.Post("http://"+a.Addr()+"/slack/events", "application/json", strings.NewReader(body))
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)
	if handlerCalled {
		t.Fatal("handler should not be called for bot messages")
	}
}

func TestSlackAdapterSend(t *testing.T) {
	var sentPayload map[string]string
	slackAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentPayload)
		if r.Header.Get("Authorization") != "Bearer xoxb-test" {
			t.Fatal("expected auth header")
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer slackAPI.Close()

	a := New("127.0.0.1:0", "xoxb-test", "t", "a")
	a.SetSlackAPI(slackAPI.URL)

	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)

	err := a.Send(ctx, channel.OutboundMessage{ChannelID: "C456", Text: "Hello from Cyntr"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sentPayload["text"] != "Hello from Cyntr" {
		t.Fatalf("expected message, got %v", sentPayload)
	}
	if sentPayload["channel"] != "C456" {
		t.Fatalf("expected channel, got %v", sentPayload)
	}
}

func TestSlackAdapterBadJSON(t *testing.T) {
	a := New("127.0.0.1:0", "xoxb-test", "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, _ := http.Post("http://"+a.Addr()+"/slack/events", "application/json", strings.NewReader("{bad"))
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
