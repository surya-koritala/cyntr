package line

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

var logger = log.Default().WithModule("channel_line")

// Adapter is a webhook-based channel adapter for the LINE Messaging API.
// Inbound requests are verified fail-closed: the X-Line-Signature header must
// equal base64(HMAC-SHA256(channelSecret, rawBody)). Without a configured
// channel secret, or with a missing/wrong signature, requests are rejected 401.
type Adapter struct {
	listenAddr    string
	accessToken   string // LINE channel access token (for outbound API calls)
	channelSecret string // LINE channel secret (for inbound signature verification)
	tenant        string // default tenant for this LINE channel
	agent         string // default agent name
	handler       channel.InboundHandler
	listener      net.Listener
	server        *http.Server
	client        *http.Client
	apiURL        string // base URL for the LINE Messaging API (override for testing)
}

// New constructs a LINE adapter. The channel secret is set separately via
// SetChannelSecret (mirroring the slack SetSigningSecret convention) so the
// adapter fails closed until it is configured.
func New(listenAddr, accessToken, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr:  listenAddr,
		accessToken: accessToken,
		tenant:      tenant,
		agent:       agent,
		client:      &http.Client{},
		apiURL:      "https://api.line.me/v2/bot",
	}
}

// SetChannelSecret sets the LINE channel secret used to verify that inbound
// requests genuinely came from LINE. Without it, inbound requests are rejected.
func (a *Adapter) SetChannelSecret(secret string) { a.channelSecret = secret }

// SetAPIURL overrides the LINE Messaging API base URL (for testing with httptest).
func (a *Adapter) SetAPIURL(url string) { a.apiURL = url }

func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *Adapter) Name() string { return "line" }

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/line/webhook", a.handleWebhook)
	ln, err := net.Listen("tcp", a.listenAddr)
	if err != nil {
		return fmt.Errorf("line listen: %w", err)
	}
	a.listener = ln
	a.server = &http.Server{Handler: mux}
	go a.server.Serve(ln)
	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {
	if a.server != nil {
		return a.server.Shutdown(ctx)
	}
	return nil
}

// verifySignature validates the X-Line-Signature header: it must equal
// base64(HMAC-SHA256(channelSecret, rawBody)). Fails closed when no channel
// secret is configured.
func (a *Adapter) verifySignature(r *http.Request, body []byte) bool {
	if a.channelSecret == "" {
		return false
	}
	sig := r.Header.Get("X-Line-Signature")
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(a.channelSecret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(sig), []byte(expected))
}

func (a *Adapter) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read error", 500)
		return
	}

	// Verify the request genuinely came from LINE before processing anything.
	if !a.verifySignature(r, body) {
		http.Error(w, "unauthorized", 401)
		return
	}

	var payload struct {
		Events []struct {
			Type       string `json:"type"`
			ReplyToken string `json:"replyToken"`
			Source     struct {
				Type    string `json:"type"`
				UserID  string `json:"userId"`
				GroupID string `json:"groupId"`
				RoomID  string `json:"roomId"`
			} `json:"source"`
			Message struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"message"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}

	// ACK immediately — LINE expects a fast 200; process asynchronously.
	w.WriteHeader(200)

	for _, ev := range payload.Events {
		if ev.Type != "message" || ev.Message.Type != "text" {
			continue
		}
		// Prefer group/room ID as the conversation channel when present.
		channelID := ev.Source.UserID
		if ev.Source.GroupID != "" {
			channelID = ev.Source.GroupID
		} else if ev.Source.RoomID != "" {
			channelID = ev.Source.RoomID
		}
		go func(channelID, userID, text, replyToken string) {
			response, err := a.handler(channel.InboundMessage{
				Channel:   "line",
				ChannelID: channelID,
				UserID:    userID,
				Text:      text,
				Tenant:    a.tenant,
				Agent:     a.agent,
			})
			if err != nil {
				logger.Error("line message handler failed", map[string]any{
					"channel_id": channelID, "user_id": userID, "error": err.Error(),
				})
				return
			}
			if response == "" {
				return
			}
			a.Send(context.Background(), channel.OutboundMessage{
				Channel:   "line",
				ChannelID: channelID,
				Text:      response,
				ThreadTS:  replyToken,
			})
		}(channelID, ev.Source.UserID, ev.Message.Text, ev.ReplyToken)
	}
}

// Send delivers a message via the LINE Messaging API. When the outbound message
// carries a reply token (in ThreadTS) it uses the reply endpoint; otherwise it
// falls back to the push endpoint addressed to the channel ID.
func (a *Adapter) Send(ctx context.Context, msg channel.OutboundMessage) error {
	messages := []map[string]string{{"type": "text", "text": msg.Text}}

	var endpoint string
	var payload map[string]any
	if msg.ThreadTS != "" {
		endpoint = a.apiURL + "/message/reply"
		payload = map[string]any{"replyToken": msg.ThreadTS, "messages": messages}
	} else {
		endpoint = a.apiURL + "/message/push"
		payload = map[string]any{"to": msg.ChannelID, "messages": messages}
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.accessToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("line send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("line API error %d: %s", resp.StatusCode, b)
	}
	return nil
}
