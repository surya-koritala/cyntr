package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/channel"
	"github.com/cyntr-dev/cyntr/modules/channel/discord"
	"github.com/cyntr-dev/cyntr/modules/channel/email"
	"github.com/cyntr-dev/cyntr/modules/channel/slack"
	"github.com/cyntr-dev/cyntr/modules/channel/teams"
	"github.com/cyntr-dev/cyntr/modules/channel/telegram"
	"github.com/cyntr-dev/cyntr/modules/channel/webhook"
	"github.com/cyntr-dev/cyntr/modules/channel/whatsapp"
)

func TestAllChannelAdaptersEndToEnd(t *testing.T) {
	// Mock API servers for outbound messages
	slackAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer slackAPI.Close()

	teamsAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer teamsAPI.Close()

	telegramAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer telegramAPI.Close()

	discordAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer discordAPI.Close()

	whatsappAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"messages": []map[string]string{{"id": "msg_1"}}})
	}))
	defer whatsappAPI.Close()

	ctx := context.Background()

	// === Slack ===
	t.Run("Slack", func(t *testing.T) {
		received := make(chan string, 1)
		a := slack.New("127.0.0.1:0", "xoxb-test", "demo", "assistant")
		a.SetSlackAPI(slackAPI.URL)
		if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
			received <- msg.Text
			return "Slack reply", nil
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		defer a.Stop(ctx)
		time.Sleep(100 * time.Millisecond)

		// URL verification
		resp, err := http.Post("http://"+a.Addr()+"/slack/events", "application/json",
			strings.NewReader(`{"type":"url_verification","challenge":"test"}`))
		if err != nil {
			t.Fatalf("verification request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("verification failed: status %d", resp.StatusCode)
		}

		// Send inbound message
		http.Post("http://"+a.Addr()+"/slack/events", "application/json",
			strings.NewReader(`{"type":"event_callback","event":{"type":"message","user":"U1","text":"Hello Slack","channel":"C1"}}`))

		select {
		case txt := <-received:
			if txt != "Hello Slack" {
				t.Fatalf("got %q, want %q", txt, "Hello Slack")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for Slack message")
		}

		// Send outbound
		if err := a.Send(ctx, channel.OutboundMessage{ChannelID: "C1", Text: "Reply"}); err != nil {
			t.Fatalf("send: %v", err)
		}
	})

	// === Teams ===
	t.Run("Teams", func(t *testing.T) {
		received := make(chan string, 1)
		a := teams.New("127.0.0.1:0", "app-id", "secret", "demo", "assistant")
		a.SetServiceURL(teamsAPI.URL)
		if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
			received <- msg.Text
			return "Teams reply", nil
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		defer a.Stop(ctx)
		time.Sleep(100 * time.Millisecond)

		http.Post("http://"+a.Addr()+"/teams/messages", "application/json",
			strings.NewReader(`{"type":"message","text":"Hello Teams","from":{"id":"U1"},"conversation":{"id":"C1"},"serviceUrl":"`+teamsAPI.URL+`"}`))

		select {
		case txt := <-received:
			if txt != "Hello Teams" {
				t.Fatalf("got %q, want %q", txt, "Hello Teams")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for Teams message")
		}
	})

	// === Telegram ===
	t.Run("Telegram", func(t *testing.T) {
		received := make(chan string, 1)
		a := telegram.New("127.0.0.1:0", "bot-token", "demo", "assistant")
		a.SetAPIURL(telegramAPI.URL)
		if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
			received <- msg.Text
			return "Telegram reply", nil
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		defer a.Stop(ctx)
		time.Sleep(100 * time.Millisecond)

		http.Post("http://"+a.Addr()+"/telegram/webhook", "application/json",
			strings.NewReader(`{"message":{"chat":{"id":123},"from":{"id":456},"text":"Hello Telegram"}}`))

		select {
		case txt := <-received:
			if txt != "Hello Telegram" {
				t.Fatalf("got %q, want %q", txt, "Hello Telegram")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for Telegram message")
		}
	})

	// === Discord ===
	t.Run("Discord", func(t *testing.T) {
		// PING verification
		a1 := discord.New("127.0.0.1:0", "bot-token", "demo", "assistant")
		a1.SetAPIURL(discordAPI.URL)
		if err := a1.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "Discord reply", nil }); err != nil {
			t.Fatalf("start: %v", err)
		}
		defer a1.Stop(ctx)
		time.Sleep(100 * time.Millisecond)

		resp, err := http.Post("http://"+a1.Addr()+"/discord/interactions", "application/json",
			strings.NewReader(`{"type":1}`))
		if err != nil {
			t.Fatalf("ping request: %v", err)
		}
		var pingResult map[string]int
		json.NewDecoder(resp.Body).Decode(&pingResult)
		resp.Body.Close()
		if pingResult["type"] != 1 {
			t.Fatalf("expected ping response type 1, got %v", pingResult)
		}

		// APPLICATION_COMMAND
		received := make(chan string, 1)
		a2 := discord.New("127.0.0.1:0", "bot-token", "demo", "assistant")
		a2.SetAPIURL(discordAPI.URL)
		if err := a2.Start(ctx, func(msg channel.InboundMessage) (string, error) {
			received <- msg.Text
			return "reply", nil
		}); err != nil {
			t.Fatalf("start a2: %v", err)
		}
		defer a2.Stop(ctx)
		time.Sleep(100 * time.Millisecond)

		http.Post("http://"+a2.Addr()+"/discord/interactions", "application/json",
			strings.NewReader(`{"type":2,"data":{"name":"ask"},"channel_id":"C1","member":{"user":{"id":"U1"}}}`))

		select {
		case txt := <-received:
			if txt != "ask" {
				t.Fatalf("got %q, want %q", txt, "ask")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for Discord command")
		}
	})

	// === WhatsApp ===
	t.Run("WhatsApp", func(t *testing.T) {
		received := make(chan string, 1)
		a := whatsapp.New("127.0.0.1:0", "token", "phone123", "verify-token", "demo", "assistant")
		a.SetAPIURL(whatsappAPI.URL)
		if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
			received <- msg.Text
			return "WhatsApp reply", nil
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		defer a.Stop(ctx)
		time.Sleep(100 * time.Millisecond)

		// Verification challenge
		resp, err := http.Get("http://" + a.Addr() + "/whatsapp/webhook?hub.mode=subscribe&hub.verify_token=verify-token&hub.challenge=test123")
		if err != nil {
			t.Fatalf("verification request: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("verification failed: status %d", resp.StatusCode)
		}

		// Inbound message
		http.Post("http://"+a.Addr()+"/whatsapp/webhook", "application/json",
			strings.NewReader(`{"entry":[{"changes":[{"value":{"messages":[{"from":"1234","text":{"body":"Hello WhatsApp"},"type":"text"}]}}]}]}`))

		select {
		case txt := <-received:
			if txt != "Hello WhatsApp" {
				t.Fatalf("got %q, want %q", txt, "Hello WhatsApp")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for WhatsApp message")
		}
	})

	// === Email ===
	t.Run("Email", func(t *testing.T) {
		received := make(chan string, 1)
		a := email.New("127.0.0.1:0", "smtp.test", "587", "bot@cyntr.dev", "demo", "assistant")
		// Inject a no-op send function so tests don't need a real SMTP server
		a.SetSendFunc(func(addr string, from string, to []string, msg []byte) error { return nil })
		if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
			received <- msg.Text
			return "Email reply", nil
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		defer a.Stop(ctx)
		time.Sleep(100 * time.Millisecond)

		http.Post("http://"+a.Addr()+"/email/inbound", "application/json",
			strings.NewReader(`{"from":"user@corp.com","to":"bot@cyntr.dev","subject":"Help","body":"Hello Email"}`))

		select {
		case txt := <-received:
			if txt != "Hello Email" {
				t.Fatalf("got %q, want %q", txt, "Hello Email")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for Email message")
		}
	})

	// === Webhook ===
	t.Run("Webhook", func(t *testing.T) {
		received := make(chan string, 1)
		a := webhook.New("127.0.0.1:0")
		if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
			received <- msg.Text
			return "Webhook reply", nil
		}); err != nil {
			t.Fatalf("start: %v", err)
		}
		defer a.Stop(ctx)
		time.Sleep(100 * time.Millisecond)

		resp, err := http.Post("http://"+a.Addr()+"/webhook", "application/json",
			strings.NewReader(`{"tenant":"demo","agent":"assistant","user_id":"U1","channel_id":"C1","text":"Hello Webhook"}`))
		if err != nil {
			t.Fatalf("webhook post: %v", err)
		}
		resp.Body.Close()

		select {
		case txt := <-received:
			if txt != "Hello Webhook" {
				t.Fatalf("got %q, want %q", txt, "Hello Webhook")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for Webhook message")
		}
	})
}

// TestAllProvidersMocked tests that mock LLM API servers respond as expected.
func TestAllProvidersMocked(t *testing.T) {
	anthropicServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "Claude response"}},
		})
	}))
	defer anthropicServer.Close()

	openaiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "GPT response"}}},
		})
	}))
	defer openaiServer.Close()

	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{"content": map[string]any{"parts": []map[string]any{{"text": "Gemini response"}}}}},
		})
	}))
	defer geminiServer.Close()

	ollamaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"role": "assistant", "content": "Ollama response"},
		})
	}))
	defer ollamaServer.Close()

	for _, tc := range []struct {
		name string
		url  string
	}{
		{"Anthropic mock", anthropicServer.URL},
		{"OpenAI mock", openaiServer.URL},
		{"Gemini mock", geminiServer.URL},
		{"Ollama mock", ollamaServer.URL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(tc.url, "application/json", strings.NewReader("{}"))
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != 200 {
				t.Fatalf("expected 200, got %d", resp.StatusCode)
			}
		})
	}
}
