package mattermost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

func TestMattermostAdapterInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestMattermostAdapterName(t *testing.T) {
	if New("", "", "", "").Name() != "mattermost" {
		t.Fatal("expected mattermost")
	}
}

func TestMattermostSend(t *testing.T) {
	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected application/json, got %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		w.WriteHeader(200)
	}))
	defer server.Close()

	a := New("127.0.0.1:0", server.URL, "demo", "assistant")
	ctx := context.Background()
	if err := a.Send(ctx, channel.OutboundMessage{ChannelID: "town-square", Text: "hello from cyntr"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got["text"] != "hello from cyntr" {
		t.Fatalf("expected text, got %v", got)
	}
	if got["channel"] != "town-square" {
		t.Fatalf("expected channel, got %v", got)
	}
}

func TestMattermostInbound(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)
	a := New("127.0.0.1:0", "", "tenantA", "assistant")
	a.SetCommandToken("s3cr3t-token")
	ctx := context.Background()
	if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "got it", nil
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.Stop(ctx)
	time.Sleep(50 * time.Millisecond)

	form := url.Values{}
	form.Set("text", "ping")
	form.Set("user_name", "alice")
	form.Set("channel_name", "town-square")
	form.Set("token", "s3cr3t-token")

	resp, err := http.Post(
		"http://"+a.Addr()+"/mattermost/command",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["text"] != "got it" {
		t.Fatalf("expected response text, got %v", body)
	}

	select {
	case msg := <-received:
		if msg.Text != "ping" {
			t.Fatalf("expected ping, got %q", msg.Text)
		}
		if msg.UserID != "alice" {
			t.Fatalf("expected alice, got %q", msg.UserID)
		}
		if msg.ChannelID != "town-square" {
			t.Fatalf("expected town-square, got %q", msg.ChannelID)
		}
		if msg.Tenant != "tenantA" {
			t.Fatalf("expected tenantA, got %q", msg.Tenant)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound message")
	}
}

func TestMattermostInboundRejectsBadToken(t *testing.T) {
	a := New("127.0.0.1:0", "", "tenantA", "assistant")
	a.SetCommandToken("s3cr3t-token")
	ctx := context.Background()
	if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		t.Fatal("handler must not run for unauthorized request")
		return "", nil
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.Stop(ctx)
	time.Sleep(50 * time.Millisecond)

	cases := []struct {
		name  string
		token string
	}{
		{"missing token", ""},
		{"wrong token", "nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{}
			form.Set("text", "ping")
			form.Set("user_name", "alice")
			form.Set("channel_name", "town-square")
			if tc.token != "" {
				form.Set("token", tc.token)
			}

			resp, err := http.Post(
				"http://"+a.Addr()+"/mattermost/command",
				"application/x-www-form-urlencoded",
				strings.NewReader(form.Encode()),
			)
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != 401 {
				t.Fatalf("expected 401, got %d", resp.StatusCode)
			}
		})
	}
}

func TestMattermostSendNoWebhook(t *testing.T) {
	a := New("127.0.0.1:0", "", "t", "a")
	err := a.Send(context.Background(), channel.OutboundMessage{ChannelID: "c", Text: "x"})
	if err == nil {
		t.Fatal("expected error when no webhook configured")
	}
}
