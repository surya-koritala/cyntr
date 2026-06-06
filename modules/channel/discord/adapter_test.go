package discord

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

// discordKeypair generates a keypair and configures the adapter with the public
// half; the returned signer posts validly-signed interactions.
func discordKeypair(t *testing.T, a *Adapter) func(addr, body string) *http.Response {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	if err := a.SetPublicKey(hex.EncodeToString(pub)); err != nil {
		t.Fatalf("set public key: %v", err)
	}
	return func(addr, body string) *http.Response {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		sig := ed25519.Sign(priv, append([]byte(ts), []byte(body)...))
		req, _ := http.NewRequest("POST", "http://"+addr+"/discord/interactions", strings.NewReader(body))
		req.Header.Set("X-Signature-Timestamp", ts)
		req.Header.Set("X-Signature-Ed25519", hex.EncodeToString(sig))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		return resp
	}
}

func TestDiscordAdapterInterface(t *testing.T) { var _ channel.ChannelAdapter = (*Adapter)(nil) }
func TestDiscordAdapterName(t *testing.T) {
	if New("", "", "", "").Name() != "discord" {
		t.Fatal()
	}
}

func TestDiscordRejectsUnsigned(t *testing.T) {
	a := New("127.0.0.1:0", "bot-token", "t", "a")
	discordKeypair(t, a)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)
	resp, _ := http.Post("http://"+a.Addr()+"/discord/interactions", "application/json", strings.NewReader(`{"type":1}`))
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("unsigned interaction should be 401, got %d", resp.StatusCode)
	}
}

func TestDiscordPingVerification(t *testing.T) {
	a := New("127.0.0.1:0", "bot-token", "t", "a")
	sign := discordKeypair(t, a)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	resp := sign(a.Addr(), `{"type":1}`)
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
	sign := discordKeypair(t, a)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { received <- msg; return "Reply!", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":2,"data":{"name":"ask"},"channel_id":"ch123","member":{"user":{"id":"u456","username":"testuser"}}}`
	sign(a.Addr(), body).Body.Close()

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
