package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

// Adapter is a webhook-based channel adapter.
// Receives messages via HTTP POST and sends responses via HTTP POST.
type Adapter struct {
	listenAddr    string
	outboundURL   string
	signingSecret string
	listener      net.Listener
	server        *http.Server
	handler       channel.InboundHandler
}

// New creates a new webhook adapter.
func New(listenAddr string) *Adapter {
	return &Adapter{listenAddr: listenAddr}
}

func (a *Adapter) Name() string { return "webhook" }

// SetOutboundURL sets the URL for outbound message delivery.
func (a *Adapter) SetOutboundURL(url string) {
	a.outboundURL = url
}

// SetSigningSecret sets the shared secret used to authenticate inbound webhook
// requests via an HMAC-SHA256 signature. Without it, inbound requests are
// rejected (fail closed), since the payload carries the tenant/agent and must
// not be trusted from an unauthenticated caller.
func (a *Adapter) SetSigningSecret(secret string) {
	a.signingSecret = secret
}

// Addr returns the actual listening address.
func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", a.handleWebhook)

	ln, err := net.Listen("tcp", a.listenAddr)
	if err != nil {
		return fmt.Errorf("webhook listen: %w", err)
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

func (a *Adapter) Send(ctx context.Context, msg channel.OutboundMessage) error {
	if a.outboundURL == "" {
		return fmt.Errorf("no outbound URL configured")
	}

	payload := map[string]string{
		"channel_id": msg.ChannelID,
		"text":       msg.Text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	resp, err := http.Post(a.outboundURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("outbound webhook returned %d", resp.StatusCode)
	}

	return nil
}

type webhookPayload struct {
	Tenant    string `json:"tenant"`
	Agent     string `json:"agent"`
	UserID    string `json:"user_id"`
	ChannelID string `json:"channel_id"`
	Text      string `json:"text"`
}

// verifySignature checks the X-Webhook-Signature header (hex HMAC-SHA256 of the
// raw body keyed by the signing secret) in constant time. Returns false when no
// secret is configured so the endpoint fails closed.
func (a *Adapter) verifySignature(r *http.Request, body []byte) bool {
	if a.signingSecret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(a.signingSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	got := r.Header.Get("X-Webhook-Signature")
	return hmac.Equal([]byte(got), []byte(expected))
}

func (a *Adapter) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Authenticate the caller before trusting the body (which carries the
	// tenant/agent). Fail closed when no secret is configured.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	if !a.verifySignature(r, body) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	msg := channel.InboundMessage{
		Channel:   "webhook",
		ChannelID: payload.ChannelID,
		UserID:    payload.UserID,
		Text:      payload.Text,
		Tenant:    payload.Tenant,
		Agent:     payload.Agent,
	}

	response, err := a.handler(msg)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"response": response})
}
