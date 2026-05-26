package signal

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

func TestSignalAdapterInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestSignalAdapterName(t *testing.T) {
	if New("", "", "", "").Name() != "signal" {
		t.Fatal("expected signal")
	}
}

func TestSignalSend(t *testing.T) {
	var got jsonRPCRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		w.WriteHeader(200)
		w.Write([]byte(`{"jsonrpc":"2.0","result":{},"id":1}`))
	}))
	defer server.Close()

	a := New("127.0.0.1:0", server.URL, "demo", "assistant")
	if err := a.Send(context.Background(), channel.OutboundMessage{ChannelID: "+15555550100", Text: "hello"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got.Method != "send" {
		t.Fatalf("expected method send, got %q", got.Method)
	}
	if got.JSONRPC != "2.0" {
		t.Fatalf("expected jsonrpc 2.0, got %q", got.JSONRPC)
	}
	if got.Params["recipient"] != "+15555550100" {
		t.Fatalf("expected recipient, got %v", got.Params)
	}
	if got.Params["message"] != "hello" {
		t.Fatalf("expected message, got %v", got.Params)
	}
}

func TestSignalInbound(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)
	a := New("127.0.0.1:0", "", "tenantA", "assistant")
	ctx := context.Background()
	if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "", nil
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.Stop(ctx)
	time.Sleep(50 * time.Millisecond)

	body := `{"envelope":{"source":"+15555550100","sourceNumber":"+15555550100","dataMessage":{"message":"hi there"}}}`
	resp, err := http.Post("http://"+a.Addr()+"/inbound/signal", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()

	select {
	case msg := <-received:
		if msg.Text != "hi there" {
			t.Fatalf("expected 'hi there', got %q", msg.Text)
		}
		if msg.ChannelID != "+15555550100" {
			t.Fatalf("expected source, got %q", msg.ChannelID)
		}
		if msg.Tenant != "tenantA" {
			t.Fatalf("expected tenantA, got %q", msg.Tenant)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound message")
	}
}

func TestSignalSendNoURL(t *testing.T) {
	a := New("127.0.0.1:0", "", "t", "a")
	err := a.Send(context.Background(), channel.OutboundMessage{ChannelID: "+1", Text: "x"})
	if err == nil {
		t.Fatal("expected error with no signal-cli URL")
	}
}
