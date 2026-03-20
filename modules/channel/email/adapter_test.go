package email

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

func TestEmailAdapterImplementsInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestEmailAdapterName(t *testing.T) {
	a := New("127.0.0.1:0", "smtp.test", "587", "bot@cyntr.dev", "t", "a")
	if a.Name() != "email" {
		t.Fatalf("got %q", a.Name())
	}
}

func TestEmailAdapterReceivesInbound(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)

	a := New("127.0.0.1:0", "smtp.test", "587", "bot@cyntr.dev", "marketing", "assistant")
	a.SetSendFunc(func(addr, from string, to []string, msg []byte) error { return nil }) // mock SMTP

	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "Got your email!", nil
	})
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"from":"user@corp.com","to":"bot@cyntr.dev","subject":"Help","body":"I need assistance"}`
	resp, err := http.Post("http://"+a.Addr()+"/email/inbound", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	select {
	case msg := <-received:
		if msg.Text != "I need assistance" {
			t.Fatalf("got %q", msg.Text)
		}
		if msg.UserID != "user@corp.com" {
			t.Fatalf("got %q", msg.UserID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestEmailAdapterSend(t *testing.T) {
	var sentTo string
	var sentBody string
	a := New("127.0.0.1:0", "smtp.test", "587", "bot@cyntr.dev", "t", "a")
	a.SetSendFunc(func(addr, from string, to []string, msg []byte) error {
		sentTo = to[0]
		sentBody = string(msg)
		return nil
	})

	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)

	err := a.Send(ctx, channel.OutboundMessage{ChannelID: "user@corp.com", Text: "Reply from agent"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sentTo != "user@corp.com" {
		t.Fatalf("got %q", sentTo)
	}
	if !strings.Contains(sentBody, "Reply from agent") {
		t.Fatalf("body missing content")
	}
}

func TestEmailAdapterBadJSON(t *testing.T) {
	a := New("127.0.0.1:0", "smtp.test", "587", "bot@cyntr.dev", "t", "a")
	a.SetSendFunc(func(string, string, []string, []byte) error { return nil })
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, _ := http.Post("http://"+a.Addr()+"/email/inbound", "application/json", strings.NewReader("{bad"))
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
