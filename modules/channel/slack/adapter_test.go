package slack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
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

const slackTestSecret = "slack-test-secret"

// postSlack POSTs body to the adapter's /slack/events with a valid signature.
func postSlack(t *testing.T, addr, body string) *http.Response {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(slackTestSecret))
	mac.Write([]byte("v0:" + ts + ":" + body))
	req, _ := http.NewRequest("POST", "http://"+addr+"/slack/events", strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

func TestSlackAdapterImplementsInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestSlackAdapterRejectsUnsigned(t *testing.T) {
	a := New("127.0.0.1:0", "xoxb-test", "t", "a")
	a.SetSigningSecret(slackTestSecret)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, _ := http.Post("http://"+a.Addr()+"/slack/events", "application/json",
		strings.NewReader(`{"type":"url_verification","challenge":"x"}`))
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("unsigned request should be 401, got %d", resp.StatusCode)
	}
}

func TestSlackAdapterName(t *testing.T) {
	a := New("127.0.0.1:0", "xoxb-test", "marketing", "assistant")
	if a.Name() != "slack" {
		t.Fatalf("expected slack, got %q", a.Name())
	}
}

func TestSlackAdapterURLVerification(t *testing.T) {
	a := New("127.0.0.1:0", "xoxb-test", "marketing", "assistant")
	a.SetSigningSecret(slackTestSecret)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"url_verification","challenge":"test-challenge-123"}`
	resp := postSlack(t, a.Addr(), body)
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
	a.SetSigningSecret(slackTestSecret)

	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "Agent reply!", nil
	})
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"event_callback","event":{"type":"message","user":"U123","text":"Hello agent","channel":"C456"}}`
	postSlack(t, a.Addr(), body).Body.Close()

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
	a.SetSigningSecret(slackTestSecret)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		handlerCalled = true
		return "", nil
	})
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"event_callback","event":{"type":"message","user":"U123","text":"bot msg","channel":"C456","bot_id":"B789"}}`
	postSlack(t, a.Addr(), body).Body.Close()

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
	a.SetSigningSecret(slackTestSecret)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	resp := postSlack(t, a.Addr(), "{bad")
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
