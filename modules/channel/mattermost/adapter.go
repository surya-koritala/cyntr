package mattermost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

var logger = log.Default().WithModule("channel_mattermost")

// Adapter integrates with a Mattermost server via incoming webhook (outbound)
// and a slash-command handler (inbound).
type Adapter struct {
	listenAddr string
	webhookURL string
	tenant     string
	agent      string
	handler    channel.InboundHandler
	listener   net.Listener
	server     *http.Server
	client     *http.Client
}

func New(listenAddr, webhookURL, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr: listenAddr,
		webhookURL: webhookURL,
		tenant:     tenant,
		agent:      agent,
		client:     &http.Client{},
	}
}

// SetWebhookURL overrides the outbound webhook URL (used in tests).
func (a *Adapter) SetWebhookURL(url string) { a.webhookURL = url }

func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *Adapter) Name() string { return "mattermost" }

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/mattermost/command", a.HandleInbound)

	ln, err := net.Listen("tcp", a.listenAddr)
	if err != nil {
		return fmt.Errorf("mattermost listen: %w", err)
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

// Send posts a message to the Mattermost incoming webhook.
func (a *Adapter) Send(ctx context.Context, msg channel.OutboundMessage) error {
	if a.webhookURL == "" {
		return fmt.Errorf("mattermost: no webhook URL configured")
	}
	payload := map[string]string{"text": msg.Text}
	if msg.ChannelID != "" {
		payload["channel"] = msg.ChannelID
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST", a.webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("mattermost send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mattermost API error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// HandleInbound handles Mattermost slash-command POSTs (application/x-www-form-urlencoded).
func (a *Adapter) HandleInbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	text := r.FormValue("text")
	userName := r.FormValue("user_name")
	channelName := r.FormValue("channel_name")
	if text == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	response, err := a.handler(channel.InboundMessage{
		Channel:   "mattermost",
		ChannelID: channelName,
		UserID:    userName,
		Text:      text,
		Tenant:    a.tenant,
		Agent:     a.agent,
	})
	if err != nil {
		logger.Error("message handler failed", map[string]any{"error": err.Error()})
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"response_type": "in_channel",
		"text":          response,
	})
}
