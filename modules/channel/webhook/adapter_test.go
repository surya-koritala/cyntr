package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

const testSecret = "test-webhook-secret"

// signedPost POSTs body to the adapter's /webhook with a valid HMAC signature.
func signedPost(t *testing.T, addr, body string) *http.Response {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write([]byte(body))
	req, _ := http.NewRequest("POST", "http://"+addr+"/webhook", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func TestWebhookAdapterImplementsInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestWebhookAdapterRejectsUnsigned(t *testing.T) {
	adapter := New("127.0.0.1:0")
	adapter.SetSigningSecret(testSecret)
	ctx := context.Background()
	adapter.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "ok", nil })
	defer adapter.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	// No signature -> rejected.
	body := `{"tenant":"victim","agent":"a","text":"x"}`
	resp, err := http.Post("http://"+adapter.Addr()+"/webhook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned request should be 401, got %d", resp.StatusCode)
	}

	// Wrong signature -> rejected.
	req, _ := http.NewRequest("POST", "http://"+adapter.Addr()+"/webhook", strings.NewReader(body))
	req.Header.Set("X-Webhook-Signature", "deadbeef")
	resp2, _ := http.DefaultClient.Do(req)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad signature should be 401, got %d", resp2.StatusCode)
	}
}

func TestWebhookAdapterReceiveMessage(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)

	adapter := New("127.0.0.1:0")
	adapter.SetSigningSecret(testSecret)
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

	// Send a signed webhook
	body := `{"tenant":"marketing","agent":"assistant","user_id":"U123","channel_id":"C456","text":"Hello"}`
	resp := signedPost(t, adapter.Addr(), body)
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
	adapter.SetSigningSecret(testSecret)
	ctx := context.Background()
	adapter.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer adapter.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	// Validly signed but malformed JSON -> 400 (passes auth, fails parse).
	resp := signedPost(t, adapter.Addr(), `{bad json`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
