package teams

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

func TestTeamsAdapterImplementsInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestTeamsAdapterName(t *testing.T) {
	a := New("127.0.0.1:0", "app-id", "secret", "t", "a")
	if a.Name() != "teams" {
		t.Fatalf("got %q", a.Name())
	}
}

func TestTeamsAdapterReceivesMessage(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)
	replyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer replyServer.Close()

	a := New("127.0.0.1:0", "app", "secret", "marketing", "assistant")
	a.SetServiceURL(replyServer.URL)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "Reply!", nil
	})
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"message","text":"Hello Teams","from":{"id":"user1"},"conversation":{"id":"conv1"},"serviceUrl":"` + replyServer.URL + `"}`
	resp, _ := http.Post("http://"+a.Addr()+"/teams/messages", "application/json", strings.NewReader(body))
	resp.Body.Close()

	select {
	case msg := <-received:
		if msg.Text != "Hello Teams" {
			t.Fatalf("got %q", msg.Text)
		}
		if msg.Channel != "teams" {
			t.Fatalf("got %q", msg.Channel)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestTeamsAdapterSend(t *testing.T) {
	var sentPayload map[string]string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentPayload)
		w.WriteHeader(200)
	}))
	defer target.Close()

	a := New("127.0.0.1:0", "app", "secret", "t", "a")
	a.SetServiceURL(target.URL)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)

	a.Send(ctx, channel.OutboundMessage{ChannelID: "conv1", Text: "From Cyntr"})
	time.Sleep(100 * time.Millisecond)
	if sentPayload["text"] != "From Cyntr" {
		t.Fatalf("got %v", sentPayload)
	}
}

func TestTeamsAdapterSkipsNonMessage(t *testing.T) {
	handlerCalled := false
	a := New("127.0.0.1:0", "app", "secret", "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { handlerCalled = true; return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"conversationUpdate","text":""}`
	resp, _ := http.Post("http://"+a.Addr()+"/teams/messages", "application/json", strings.NewReader(body))
	resp.Body.Close()
	time.Sleep(100 * time.Millisecond)
	if handlerCalled {
		t.Fatal("should not call handler for non-message")
	}
}
