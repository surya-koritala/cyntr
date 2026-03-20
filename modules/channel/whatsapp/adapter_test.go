package whatsapp

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

func TestWhatsAppAdapterInterface(t *testing.T) { var _ channel.ChannelAdapter = (*Adapter)(nil) }
func TestWhatsAppAdapterName(t *testing.T) {
	if New("", "", "", "", "", "").Name() != "whatsapp" {
		t.Fatal()
	}
}

func TestWhatsAppVerification(t *testing.T) {
	a := New("127.0.0.1:0", "token", "phone", "my-verify-token", "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://" + a.Addr() + "/whatsapp/webhook?hub.mode=subscribe&hub.verify_token=my-verify-token&hub.challenge=test123")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWhatsAppVerificationBadToken(t *testing.T) {
	a := New("127.0.0.1:0", "token", "phone", "correct", "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://" + a.Addr() + "/whatsapp/webhook?hub.mode=subscribe&hub.verify_token=wrong&hub.challenge=test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestWhatsAppReceivesMessage(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer apiServer.Close()

	a := New("127.0.0.1:0", "token", "phone123", "verify", "demo", "assistant")
	a.SetAPIURL(apiServer.URL)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { received <- msg; return "Reply!", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"entry":[{"changes":[{"value":{"messages":[{"from":"1234567890","text":{"body":"Hello WhatsApp"},"type":"text"}]}}]}]}`
	http.Post("http://"+a.Addr()+"/whatsapp/webhook", "application/json", strings.NewReader(body))

	select {
	case msg := <-received:
		if msg.Text != "Hello WhatsApp" {
			t.Fatalf("got %q", msg.Text)
		}
		if msg.UserID != "1234567890" {
			t.Fatalf("got %q", msg.UserID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestWhatsAppSend(t *testing.T) {
	var sentPayload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentPayload)
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("missing auth")
		}
		w.WriteHeader(200)
	}))
	defer apiServer.Close()

	a := New("127.0.0.1:0", "test-token", "phone123", "v", "t", "a")
	a.SetAPIURL(apiServer.URL)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)

	a.Send(ctx, channel.OutboundMessage{ChannelID: "1234567890", Text: "Hello from Cyntr"})
	time.Sleep(100 * time.Millisecond)
	textMap, _ := sentPayload["text"].(map[string]any)
	if textMap["body"] != "Hello from Cyntr" {
		t.Fatalf("got %v", sentPayload)
	}
}
