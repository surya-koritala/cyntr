package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

type Adapter struct {
	listenAddr string
	botToken   string
	tenant     string // default tenant for this Slack workspace
	agent      string // default agent name
	handler    channel.InboundHandler
	listener   net.Listener
	server     *http.Server
	client     *http.Client
	slackAPI   string // base URL for Slack API (override for testing)
	seen       sync.Map // event deduplication: event_id -> true
}

func New(listenAddr, botToken, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr: listenAddr,
		botToken:   botToken,
		tenant:     tenant,
		agent:      agent,
		client:     &http.Client{},
		slackAPI:   "https://slack.com/api",
	}
}

// SetSlackAPI overrides the Slack API URL (for testing with httptest).
func (a *Adapter) SetSlackAPI(url string) { a.slackAPI = url }

func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *Adapter) Name() string { return "slack" }

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/slack/events", a.handleEvents)

	ln, err := net.Listen("tcp", a.listenAddr)
	if err != nil {
		return fmt.Errorf("slack listen: %w", err)
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
	payload := map[string]string{
		"channel": msg.ChannelID,
		"text":    msg.Text,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", a.slackAPI+"/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.botToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack API error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// handleEvents is the Slack Events API handler.
func (a *Adapter) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", 500)
		return
	}

	// Parse the outer event wrapper
	var envelope struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		EventID   string `json:"event_id"`
		Event     struct {
			Type        string `json:"type"`
			Subtype     string `json:"subtype"`
			User        string `json:"user"`
			Text        string `json:"text"`
			Channel     string `json:"channel"`
			BotID       string `json:"bot_id"`
			ClientMsgID string `json:"client_msg_id"`
		} `json:"event"`
	}

	if err := json.Unmarshal(body, &envelope); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}

	// URL verification challenge
	if envelope.Type == "url_verification" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"challenge": envelope.Challenge})
		return
	}

	// Only handle message events
	if envelope.Type != "event_callback" || envelope.Event.Type != "message" {
		w.WriteHeader(200)
		return
	}

	// Skip bot messages to avoid loops (check both bot_id and subtype)
	if envelope.Event.BotID != "" {
		w.WriteHeader(200)
		return
	}

	// Skip messages with no user (system messages, bot messages without bot_id)
	if envelope.Event.User == "" {
		w.WriteHeader(200)
		return
	}

	// Skip message subtypes (edits, deletes, bot_message, etc.)
	if envelope.Event.Subtype != "" {
		w.WriteHeader(200)
		return
	}

	// Deduplicate using event_id OR client_msg_id OR user+text hash
	dedupeKey := envelope.EventID
	if dedupeKey == "" {
		dedupeKey = envelope.Event.ClientMsgID
	}
	if dedupeKey == "" {
		// Fallback: hash of user+channel+text
		dedupeKey = envelope.Event.User + ":" + envelope.Event.Channel + ":" + envelope.Event.Text
	}
	if _, loaded := a.seen.LoadOrStore(dedupeKey, true); loaded {
		w.WriteHeader(200)
		return
	}

	// ACK immediately — Slack retries if we don't respond within 3 seconds.
	// Process the message asynchronously to avoid duplicate responses.
	w.WriteHeader(200)

	// Deduplicate: use event_id if available, otherwise use a simple check
	eventChannel := envelope.Event.Channel
	eventUser := envelope.Event.User
	eventText := envelope.Event.Text

	go func() {
		response, err := a.handler(channel.InboundMessage{
			Channel:   "slack",
			ChannelID: eventChannel,
			UserID:    eventUser,
			Text:      eventText,
			Tenant:    a.tenant,
			Agent:     a.agent,
		})

		if err != nil {
			return
		}

		a.Send(context.Background(), channel.OutboundMessage{
			Channel:   "slack",
			ChannelID: eventChannel,
			Text:      response,
		})
	}()
}
