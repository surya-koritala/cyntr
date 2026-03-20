package telegram

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

func TestTelegramAdapterInterface(t *testing.T) { var _ channel.ChannelAdapter = (*Adapter)(nil) }
func TestTelegramAdapterName(t *testing.T) {
	if New("", "", "", "").Name() != "telegram" {
		t.Fatal()
	}
}

func TestTelegramReceivesMessage(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer apiServer.Close()

	a := New("127.0.0.1:0", "bot-token", "demo", "assistant")
	a.SetAPIURL(apiServer.URL)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { received <- msg; return "Reply!", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"message":{"chat":{"id":12345},"from":{"id":67890,"username":"testuser"},"text":"Hello Telegram"}}`
	http.Post("http://"+a.Addr()+"/telegram/webhook", "application/json", strings.NewReader(body))

	select {
	case msg := <-received:
		if msg.Text != "Hello Telegram" {
			t.Fatalf("got %q", msg.Text)
		}
		if msg.ChannelID != "12345" {
			t.Fatalf("got %q", msg.ChannelID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestTelegramSend(t *testing.T) {
	var sentPayload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentPayload)
		w.WriteHeader(200)
	}))
	defer apiServer.Close()

	a := New("127.0.0.1:0", "bot-token", "t", "a")
	a.SetAPIURL(apiServer.URL)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)

	a.Send(ctx, channel.OutboundMessage{ChannelID: "12345", Text: "From Cyntr"})
	time.Sleep(100 * time.Millisecond)
	if sentPayload["text"] != "From Cyntr" {
		t.Fatalf("got %v", sentPayload)
	}
}

func TestTelegramSkipsEmptyText(t *testing.T) {
	called := false
	a := New("127.0.0.1:0", "token", "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { called = true; return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	http.Post("http://"+a.Addr()+"/telegram/webhook", "application/json", strings.NewReader(`{"message":{"chat":{"id":1},"from":{"id":1},"text":""}}`))
	time.Sleep(100 * time.Millisecond)
	if called {
		t.Fatal("should skip empty text")
	}
}
