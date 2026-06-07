package line

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

func TestLineAdapterInterface(t *testing.T) { var _ channel.ChannelAdapter = (*Adapter)(nil) }

func TestLineAdapterName(t *testing.T) {
	if New("", "", "", "").Name() != "line" {
		t.Fatal("expected name line")
	}
}

// sign returns the LINE signature header value for a body and secret.
func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func startAdapter(t *testing.T, secret string, handler channel.InboundHandler) *Adapter {
	t.Helper()
	a := New("127.0.0.1:0", "token", "demo", "assistant")
	if secret != "" {
		a.SetChannelSecret(secret)
	}
	ctx := context.Background()
	if err := a.Start(ctx, handler); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { a.Stop(context.Background()) })
	// Wait for the listener to come up.
	for i := 0; i < 50 && a.Addr() == ""; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	return a
}

func TestLineWebhookSignature(t *testing.T) {
	const secret = "channel-secret"
	body := `{"events":[{"type":"message","replyToken":"rt-123","source":{"type":"user","userId":"U999"},"message":{"type":"text","text":"Hello LINE"}}]}`

	tests := []struct {
		name       string
		secret     string // adapter-configured secret
		sig        string // header value; "" means omit header
		wantStatus int
		wantCalled bool
	}{
		{"valid signature", secret, sign(secret, body), 200, true},
		{"missing signature", secret, "", 401, false},
		{"wrong signature", secret, sign("other-secret", body), 401, false},
		{"unset secret fails closed", "", sign(secret, body), 401, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			received := make(chan channel.InboundMessage, 1)
			a := startAdapter(t, tc.secret, func(msg channel.InboundMessage) (string, error) {
				received <- msg
				return "", nil
			})

			req, _ := http.NewRequest("POST", "http://"+a.Addr()+"/line/webhook", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tc.sig != "" {
				req.Header.Set("X-Line-Signature", tc.sig)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status: got %d want %d", resp.StatusCode, tc.wantStatus)
			}

			if tc.wantCalled {
				select {
				case msg := <-received:
					if msg.Text != "Hello LINE" {
						t.Fatalf("text: got %q", msg.Text)
					}
					if msg.Tenant != "demo" || msg.Agent != "assistant" {
						t.Fatalf("tenant/agent: got %q/%q", msg.Tenant, msg.Agent)
					}
					if msg.UserID != "U999" {
						t.Fatalf("userID: got %q", msg.UserID)
					}
					if msg.Channel != "line" {
						t.Fatalf("channel: got %q", msg.Channel)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("handler not called for valid signature")
				}
			} else {
				select {
				case <-received:
					t.Fatal("handler ran despite rejected request")
				case <-time.After(200 * time.Millisecond):
				}
			}
		})
	}
}

func TestLineSendReply(t *testing.T) {
	var gotPath string
	var gotPayload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing auth header: %q", r.Header.Get("Authorization"))
		}
		json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(200)
	}))
	defer apiServer.Close()

	a := New("127.0.0.1:0", "test-token", "t", "a")
	a.SetAPIURL(apiServer.URL)

	// Reply path: ThreadTS set => reply endpoint with replyToken.
	if err := a.Send(context.Background(), channel.OutboundMessage{ChannelID: "U1", Text: "hi", ThreadTS: "rt-abc"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotPath != "/message/reply" {
		t.Fatalf("path: got %q", gotPath)
	}
	if gotPayload["replyToken"] != "rt-abc" {
		t.Fatalf("replyToken: got %v", gotPayload["replyToken"])
	}
	msgs, _ := gotPayload["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages: got %v", gotPayload["messages"])
	}
	m0, _ := msgs[0].(map[string]any)
	if m0["type"] != "text" || m0["text"] != "hi" {
		t.Fatalf("message body: got %v", m0)
	}
}

func TestLineSendPush(t *testing.T) {
	var gotPath string
	var gotPayload map[string]any
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotPayload)
		w.WriteHeader(200)
	}))
	defer apiServer.Close()

	a := New("127.0.0.1:0", "test-token", "t", "a")
	a.SetAPIURL(apiServer.URL)

	// Push path: no ThreadTS => push endpoint addressed to channel ID.
	if err := a.Send(context.Background(), channel.OutboundMessage{ChannelID: "U42", Text: "pushed"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotPath != "/message/push" {
		t.Fatalf("path: got %q", gotPath)
	}
	if gotPayload["to"] != "U42" {
		t.Fatalf("to: got %v", gotPayload["to"])
	}
}
