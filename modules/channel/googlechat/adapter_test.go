package googlechat

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

func TestGoogleChatAdapterImplementsInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestGoogleChatAdapterName(t *testing.T) {
	a := New("127.0.0.1:0", "https://chat.googleapis.com/webhook", "marketing", "assistant")
	if a.Name() != "googlechat" {
		t.Fatalf("expected googlechat, got %q", a.Name())
	}
}

func TestGoogleChatAdapterReceivesMessage(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)

	webhookAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer webhookAPI.Close()

	a := New("127.0.0.1:0", webhookAPI.URL, "marketing", "assistant")

	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "Agent reply!", nil
	})
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"MESSAGE","eventId":"evt1","message":{"name":"spaces/123/messages/456","sender":{"name":"users/789","displayName":"Alice","type":"HUMAN"},"text":"Hello agent"},"space":{"name":"spaces/123"}}`
	resp, _ := http.Post("http://"+a.Addr()+"/googlechat/events", "application/json", strings.NewReader(body))
	resp.Body.Close()

	select {
	case msg := <-received:
		if msg.Text != "Hello agent" {
			t.Fatalf("expected message, got %q", msg.Text)
		}
		if msg.UserID != "users/789" {
			t.Fatalf("expected users/789, got %q", msg.UserID)
		}
		if msg.ChannelID != "spaces/123" {
			t.Fatalf("expected spaces/123, got %q", msg.ChannelID)
		}
		if msg.Tenant != "marketing" {
			t.Fatalf("expected marketing, got %q", msg.Tenant)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestGoogleChatAdapterSkipsBotMessages(t *testing.T) {
	handlerCalled := false
	a := New("127.0.0.1:0", "https://example.com", "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		handlerCalled = true
		return "", nil
	})
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"MESSAGE","message":{"name":"msg1","sender":{"name":"users/bot","type":"BOT"},"text":"bot msg"},"space":{"name":"spaces/1"}}`
	resp, _ := http.Post("http://"+a.Addr()+"/googlechat/events", "application/json", strings.NewReader(body))
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)
	if handlerCalled {
		t.Fatal("handler should not be called for bot messages")
	}
}

func TestGoogleChatAdapterSkipsNonMessage(t *testing.T) {
	handlerCalled := false
	a := New("127.0.0.1:0", "https://example.com", "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		handlerCalled = true
		return "", nil
	})
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"ADDED_TO_SPACE","space":{"name":"spaces/1"}}`
	resp, _ := http.Post("http://"+a.Addr()+"/googlechat/events", "application/json", strings.NewReader(body))
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)
	if handlerCalled {
		t.Fatal("handler should not be called for non-message events")
	}
}

func TestGoogleChatAdapterSend(t *testing.T) {
	var sentPayload map[string]string
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentPayload)
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer webhook.Close()

	a := New("127.0.0.1:0", webhook.URL, "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)

	err := a.Send(ctx, channel.OutboundMessage{ChannelID: "spaces/123", Text: "Hello from Cyntr"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sentPayload["text"] != "Hello from Cyntr" {
		t.Fatalf("expected message, got %v", sentPayload)
	}
}

func TestGoogleChatAdapterBadJSON(t *testing.T) {
	a := New("127.0.0.1:0", "https://example.com", "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, _ := http.Post("http://"+a.Addr()+"/googlechat/events", "application/json", strings.NewReader("{bad"))
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGoogleChatAdapterMethodNotAllowed(t *testing.T) {
	a := New("127.0.0.1:0", "https://example.com", "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, _ := http.Get("http://" + a.Addr() + "/googlechat/events")
	resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}
