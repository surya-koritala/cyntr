package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

var logger = log.Default().WithModule("channel_slack")

// TODO: Multi-workspace support planned for v0.7.0
// Would require SLACK_WORKSPACES JSON env, per-workspace tokens,
// and team_id-based routing in handleEvents.
type Adapter struct {
	listenAddr string
	botToken   string
	tenant     string // default tenant for this Slack workspace
	agent      string // default agent name
	routes     map[string]string // channel ID -> agent name overrides
	useThreads bool   // reply in threads instead of top-level messages
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

// SetRoutes configures per-channel agent routing overrides.
// The map keys are Slack channel IDs and values are agent names.
func (a *Adapter) SetRoutes(routes map[string]string) { a.routes = routes }

// SetSlackAPI overrides the Slack API URL (for testing with httptest).
func (a *Adapter) SetSlackAPI(url string) { a.slackAPI = url }

// SetUseThreads enables replying in threads instead of top-level messages.
func (a *Adapter) SetUseThreads(use bool) { a.useThreads = use }

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
	mux.HandleFunc("/slack/commands", a.handleSlashCommands)

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
	chunks := chunkMessage(msg.Text, 3900)
	for _, chunk := range chunks {
		payload := map[string]any{
			"channel": msg.ChannelID,
			"text":    chunk,
		}
		if msg.ThreadTS != "" {
			payload["thread_ts"] = msg.ThreadTS
		}
		blocks := FormatAsBlocks(chunk)
		if len(blocks) > 0 {
			payload["blocks"] = blocks
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
		resp.Body.Close()

		if resp.StatusCode != 200 {
			return fmt.Errorf("slack API error %d", resp.StatusCode)
		}
	}
	return nil
}

// addReaction adds an emoji reaction to a message (typing indicator).
func (a *Adapter) addReaction(channel, timestamp, emoji string) {
	if timestamp == "" {
		return
	}
	payload := map[string]string{"channel": channel, "timestamp": timestamp, "name": emoji}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", a.slackAPI+"/reactions.add", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.botToken)
	resp, err := a.client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
}

// removeReaction removes an emoji reaction from a message.
func (a *Adapter) removeReaction(channel, timestamp, emoji string) {
	if timestamp == "" {
		return
	}
	payload := map[string]string{"channel": channel, "timestamp": timestamp, "name": emoji}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", a.slackAPI+"/reactions.remove", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.botToken)
	resp, err := a.client.Do(req)
	if err == nil {
		resp.Body.Close()
	}
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
		Event struct {
			Type        string `json:"type"`
			Subtype     string `json:"subtype"`
			User        string `json:"user"`
			Text        string `json:"text"`
			Channel     string `json:"channel"`
			BotID       string `json:"bot_id"`
			ClientMsgID string `json:"client_msg_id"`
			TS          string `json:"ts"`
			Reaction    string `json:"reaction"`
			Item        struct {
				Type    string `json:"type"`
				Channel string `json:"channel"`
				TS      string `json:"ts"`
			} `json:"item"`
			Files []struct {
				ID                 string `json:"id"`
				Name               string `json:"name"`
				MimeType           string `json:"mimetype"`
				URLPrivateDownload string `json:"url_private_download"`
			} `json:"files"`
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

	// Handle reaction_added events
	if envelope.Type == "event_callback" && envelope.Event.Type == "reaction_added" {
		a.handleReaction(envelope.Event)
		w.WriteHeader(200)
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

	// Reject Slack retries immediately — Slack sends X-Slack-Retry-Num header
	// when it doesn't get a response within 3 seconds. We already ACK'd the
	// first request and are processing it async.
	if r.Header.Get("X-Slack-Retry-Num") != "" {
		w.WriteHeader(200)
		return
	}

	// Deduplicate using client_msg_id (stable across retries) > event_id > user+text hash
	dedupeKey := envelope.Event.ClientMsgID
	if dedupeKey == "" {
		dedupeKey = envelope.EventID
	}
	if dedupeKey == "" {
		dedupeKey = envelope.Event.User + ":" + envelope.Event.Channel + ":" + envelope.Event.Text
	}
	if _, loaded := a.seen.LoadOrStore(dedupeKey, true); loaded {
		w.WriteHeader(200)
		return
	}

	// ACK immediately — Slack retries if we don't respond within 3 seconds.
	// Process the message asynchronously to avoid duplicate responses.
	w.WriteHeader(200)

	eventChannel := envelope.Event.Channel
	eventUser := envelope.Event.User
	eventText := envelope.Event.Text
	eventTS := envelope.Event.TS

	// Append file attachment info to message text (B2: File Upload Handling)
	if len(envelope.Event.Files) > 0 {
		for _, f := range envelope.Event.Files {
			eventText += fmt.Sprintf("\n[Attached file: %s (%s)]", f.Name, f.MimeType)
		}
	}

	go func() {
		// Show typing indicator — add reaction to the user's message
		a.addReaction(eventChannel, eventTS, "hourglass_flowing_sand")

		agentName := a.agent
		if a.routes != nil {
			if routed, ok := a.routes[eventChannel]; ok {
				agentName = routed
			}
		}

		response, err := a.handler(channel.InboundMessage{
			Channel:   "slack",
			ChannelID: eventChannel,
			UserID:    eventUser,
			Text:      eventText,
			Tenant:    a.tenant,
			Agent:     agentName,
		})

		// Remove typing indicator
		a.removeReaction(eventChannel, eventTS, "hourglass_flowing_sand")

		// Determine thread timestamp for reply
		var threadTS string
		if a.useThreads && eventTS != "" {
			threadTS = eventTS
		}

		if err != nil {
			logger.Error("slack message handler failed", map[string]any{
				"channel_id": eventChannel, "user_id": eventUser, "error": err.Error(),
			})
			a.Send(context.Background(), channel.OutboundMessage{
				Channel:   "slack",
				ChannelID: eventChannel,
				Text:      "Sorry, I encountered an error processing your request.",
				ThreadTS:  threadTS,
			})
			return
		}

		a.Send(context.Background(), channel.OutboundMessage{
			Channel:   "slack",
			ChannelID: eventChannel,
			Text:      response,
			ThreadTS:  threadTS,
		})
	}()
}

// handleReaction processes reaction_added events (B6: Reaction Commands).
func (a *Adapter) handleReaction(event struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype"`
	User        string `json:"user"`
	Text        string `json:"text"`
	Channel     string `json:"channel"`
	BotID       string `json:"bot_id"`
	ClientMsgID string `json:"client_msg_id"`
	TS          string `json:"ts"`
	Reaction    string `json:"reaction"`
	Item        struct {
		Type    string `json:"type"`
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	} `json:"item"`
	Files []struct {
		ID                 string `json:"id"`
		Name               string `json:"name"`
		MimeType           string `json:"mimetype"`
		URLPrivateDownload string `json:"url_private_download"`
	} `json:"files"`
}) {
	switch event.Reaction {
	case "white_check_mark":
		// Approve — would need to look up approval ID from message timestamp
		logger.Info("approval reaction received", map[string]any{"channel": event.Item.Channel, "ts": event.Item.TS})
	case "x":
		logger.Info("denial reaction received", map[string]any{"channel": event.Item.Channel, "ts": event.Item.TS})
	}
}

// handleSlashCommands processes Slack slash command requests (B3: Slash Commands).
func (a *Adapter) handleSlashCommands(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	r.ParseForm()
	command := r.FormValue("command")
	text := strings.TrimSpace(r.FormValue("text"))

	var response string
	switch {
	case text == "status" || command == "/cyntr" && text == "status":
		response = "Cyntr is running. Use the dashboard for full status."
	case strings.HasPrefix(text, "switch "):
		agentName := strings.TrimPrefix(text, "switch ")
		if a.routes == nil {
			a.routes = make(map[string]string)
		}
		channelID := r.FormValue("channel_id")
		a.routes[channelID] = agentName
		response = fmt.Sprintf("Switched this channel to agent: %s", agentName)
	case text == "clear" || text == "reset":
		response = "Session cleared. Starting fresh conversation."
	default:
		response = "Available commands: status, switch <agent>, clear"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"response_type": "ephemeral", "text": response})
}
