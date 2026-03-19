package webhook

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

func TestWebhookAdapterImplementsInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestWebhookAdapterReceiveMessage(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)

	adapter := New("127.0.0.1:0")
	ctx := context.Background()

	err := adapter.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "Got it!", nil
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer adapter.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	// Send a webhook
	body := `{"tenant":"marketing","agent":"assistant","user_id":"U123","channel_id":"C456","text":"Hello"}`
	resp, err := http.Post("http://"+adapter.Addr()+"/webhook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["response"] != "Got it!" {
		t.Fatalf("expected 'Got it!', got %v", result)
	}

	select {
	case msg := <-received:
		if msg.Text != "Hello" {
			t.Fatalf("expected 'Hello', got %q", msg.Text)
		}
		if msg.Tenant != "marketing" {
			t.Fatalf("expected marketing, got %q", msg.Tenant)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestWebhookAdapterSend(t *testing.T) {
	// Create a test server to receive outbound messages
	var sentBody map[string]string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentBody)
		w.WriteHeader(200)
	}))
	defer target.Close()

	adapter := New("127.0.0.1:0")
	adapter.SetOutboundURL(target.URL)

	ctx := context.Background()
	adapter.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer adapter.Stop(ctx)

	err := adapter.Send(ctx, channel.OutboundMessage{
		Channel:   "webhook",
		ChannelID: "C456",
		Text:      "Hello from agent!",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if sentBody["text"] != "Hello from agent!" {
		t.Fatalf("expected message, got %v", sentBody)
	}
}

func TestWebhookAdapterName(t *testing.T) {
	a := New("127.0.0.1:0")
	if a.Name() != "webhook" {
		t.Fatalf("expected webhook, got %q", a.Name())
	}
}

func TestWebhookAdapterBadJSON(t *testing.T) {
	adapter := New("127.0.0.1:0")
	ctx := context.Background()
	adapter.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer adapter.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Post("http://"+adapter.Addr()+"/webhook", "application/json", strings.NewReader(`{bad json`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
