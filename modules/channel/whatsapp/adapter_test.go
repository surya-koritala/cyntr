package whatsapp

import (
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

// signWhatsApp computes the X-Hub-Signature-256 header value Meta sends:
// "sha256=" + hex(HMAC-SHA256(appSecret, rawBody)).
func signWhatsApp(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// postSigned sends a signed inbound webhook POST to the adapter.
func postSigned(addr, secret, body string) (*http.Response, error) {
	req, err := http.NewRequest("POST", "http://"+addr+"/whatsapp/webhook", strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("X-Hub-Signature-256", signWhatsApp(secret, body))
	}
	return http.DefaultClient.Do(req)
}

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

	const secret = "app-secret"
	a := New("127.0.0.1:0", "token", "phone123", "verify", "demo", "assistant")
	a.SetAPIURL(apiServer.URL)
	a.SetAppSecret(secret)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { received <- msg; return "Reply!", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"entry":[{"changes":[{"value":{"messages":[{"from":"1234567890","text":{"body":"Hello WhatsApp"},"type":"text"}]}}]}]}`
	if _, err := postSigned(a.Addr(), secret, body); err != nil {
		t.Fatalf("request failed: %v", err)
	}

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

func TestWhatsAppRejectsUnsignedAndBadSignature(t *testing.T) {
	const secret = "app-secret"
	a := New("127.0.0.1:0", "token", "phone123", "verify", "demo", "assistant")
	a.SetAppSecret(secret)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"entry":[{"changes":[{"value":{"messages":[{"from":"1","text":{"body":"hi"},"type":"text"}]}}]}]}`

	tests := []struct {
		name      string
		setHeader func(r *http.Request)
	}{
		{"missing signature", func(r *http.Request) {}},
		{"empty signature", func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", "") }},
		{"malformed signature", func(r *http.Request) { r.Header.Set("X-Hub-Signature-256", "sha256=zzzz") }},
		{"wrong secret", func(r *http.Request) {
			r.Header.Set("X-Hub-Signature-256", signWhatsApp("wrong-secret", body))
		}},
		{"signature over different body", func(r *http.Request) {
			r.Header.Set("X-Hub-Signature-256", signWhatsApp(secret, body+"tampered"))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", "http://"+a.Addr()+"/whatsapp/webhook", strings.NewReader(body))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			tt.setHeader(req)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", resp.StatusCode)
			}
		})
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
