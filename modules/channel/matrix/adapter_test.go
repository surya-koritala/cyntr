package matrix

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

func TestMatrixAdapterInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestMatrixAdapterName(t *testing.T) {
	if New("", "", "", "", "").Name() != "matrix" {
		t.Fatal("expected matrix")
	}
}

func TestMatrixSend(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotContentType string
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.WriteHeader(200)
		w.Write([]byte(`{"event_id":"$abc"}`))
	}))
	defer server.Close()

	a := New("127.0.0.1:0", server.URL, "syt_test_token", "demo", "assistant")
	if err := a.Send(context.Background(), channel.OutboundMessage{ChannelID: "!room:matrix.org", Text: "hello matrix"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotMethod != "PUT" {
		t.Fatalf("expected PUT, got %s", gotMethod)
	}
	if !strings.HasPrefix(gotPath, "/_matrix/client/v3/rooms/!room:matrix.org/send/m.room.message/") {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotAuth != "Bearer syt_test_token" {
		t.Fatalf("unexpected auth %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected content type %q", gotContentType)
	}
	if gotBody["msgtype"] != "m.text" {
		t.Fatalf("expected m.text, got %v", gotBody)
	}
	if gotBody["body"] != "hello matrix" {
		t.Fatalf("expected body, got %v", gotBody)
	}
}

func TestMatrixSendTxnIDsUnique(t *testing.T) {
	seen := make(map[string]bool)
	dup := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		txnID := parts[len(parts)-1]
		if seen[txnID] {
			dup = true
		}
		seen[txnID] = true
		w.WriteHeader(200)
	}))
	defer server.Close()

	a := New("127.0.0.1:0", server.URL, "token", "t", "a")
	for i := 0; i < 5; i++ {
		if err := a.Send(context.Background(), channel.OutboundMessage{ChannelID: "!r:m.org", Text: "x"}); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	if dup {
		t.Fatal("expected unique txn IDs across sends")
	}
}

func TestMatrixInbound(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)
	a := New("127.0.0.1:0", "", "", "tenantA", "assistant")
	ctx := context.Background()
	if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "", nil
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.Stop(ctx)
	time.Sleep(50 * time.Millisecond)

	body := `{"type":"m.room.message","sender":"@alice:matrix.org","room_id":"!room:matrix.org","content":{"msgtype":"m.text","body":"hello cyntr"}}`
	resp, err := http.Post("http://"+a.Addr()+"/inbound/matrix", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()

	select {
	case msg := <-received:
		if msg.Text != "hello cyntr" {
			t.Fatalf("expected text, got %q", msg.Text)
		}
		if msg.ChannelID != "!room:matrix.org" {
			t.Fatalf("expected room, got %q", msg.ChannelID)
		}
		if msg.UserID != "@alice:matrix.org" {
			t.Fatalf("expected sender, got %q", msg.UserID)
		}
		if msg.Tenant != "tenantA" {
			t.Fatalf("expected tenantA, got %q", msg.Tenant)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound message")
	}
}

func TestMatrixSendMissingConfig(t *testing.T) {
	a := New("127.0.0.1:0", "", "", "t", "a")
	err := a.Send(context.Background(), channel.OutboundMessage{ChannelID: "!r:m.org", Text: "x"})
	if err == nil {
		t.Fatal("expected error with missing config")
	}
}
