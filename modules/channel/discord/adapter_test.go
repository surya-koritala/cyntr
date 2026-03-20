package discord

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

func TestDiscordAdapterInterface(t *testing.T) { var _ channel.ChannelAdapter = (*Adapter)(nil) }
func TestDiscordAdapterName(t *testing.T) {
	if New("", "", "", "").Name() != "discord" {
		t.Fatal()
	}
}

func TestDiscordPingVerification(t *testing.T) {
	a := New("127.0.0.1:0", "bot-token", "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Post("http://"+a.Addr()+"/discord/interactions", "application/json", strings.NewReader(`{"type":1}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	var result map[string]int
	json.NewDecoder(resp.Body).Decode(&result)
	if result["type"] != 1 {
		t.Fatalf("expected ping response, got %v", result)
	}
}

func TestDiscordReceivesCommand(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bot test-token" {
			t.Fatal("missing auth")
		}
		w.WriteHeader(200)
	}))
	defer apiServer.Close()

	a := New("127.0.0.1:0", "test-token", "demo", "assistant")
	a.SetAPIURL(apiServer.URL)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { received <- msg; return "Reply!", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":2,"data":{"name":"ask"},"channel_id":"ch123","member":{"user":{"id":"u456","username":"testuser"}}}`
	http.Post("http://"+a.Addr()+"/discord/interactions", "application/json", strings.NewReader(body))

	select {
	case msg := <-received:
		if msg.Text != "ask" {
			t.Fatalf("got %q", msg.Text)
		}
		if msg.ChannelID != "ch123" {
			t.Fatalf("got %q", msg.ChannelID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestDiscordSend(t *testing.T) {
	var sentPayload map[string]string
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentPayload)
		w.WriteHeader(200)
	}))
	defer apiServer.Close()

	a := New("127.0.0.1:0", "token", "t", "a")
	a.SetAPIURL(apiServer.URL)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)

	a.Send(ctx, channel.OutboundMessage{ChannelID: "ch123", Text: "From Cyntr"})
	time.Sleep(100 * time.Millisecond)
	if sentPayload["content"] != "From Cyntr" {
		t.Fatalf("got %v", sentPayload)
	}
}
