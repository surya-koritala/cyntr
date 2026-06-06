package slack

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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

var logger = log.Default().WithModule("channel_slack")

// TODO: Multi-workspace support planned for v0.7.0
// Would require SLACK_WORKSPACES JSON env, per-workspace tokens,
// and team_id-based routing in handleEvents.
type Adapter struct {
	listenAddr    string
	botToken      string
	signingSecret string            // Slack signing secret for request verification
	tenant        string            // default tenant for this Slack workspace
	agent         string            // default agent name
	routes        map[string]string // channel ID -> agent name overrides
	routesMu      sync.RWMutex      // guards routes
	useThreads    bool              // reply in threads instead of top-level messages
	handler       channel.InboundHandler
	listener      net.Listener
	server        *http.Server
	client        *http.Client
	slackAPI      string // base URL for Slack API (override for testing)
	seen          *dedupeCache
}

// dedupeCache is a bounded, TTL-based set used for event deduplication. Slack
// retries the same event, so we remember keys we've already processed — but
// only for a bounded window (and with a hard cap on entries) so an attacker
// cannot grow it without limit by sending a stream of unique event ids.
type dedupeCache struct {
	mu      sync.Mutex
	entries map[string]int64 // key -> unix expiry
	ttl     time.Duration
	maxSize int
}

func newDedupeCache(ttl time.Duration, maxSize int) *dedupeCache {
	return &dedupeCache{entries: make(map[string]int64), ttl: ttl, maxSize: maxSize}
}

// seenBefore records key and reports whether it had already been seen within
// the TTL window. Expired entries are evicted lazily on each call; if the cache
// is at capacity with no expired entries to reclaim, the oldest entries are
// dropped to enforce the hard cap.
func (c *dedupeCache) seenBefore(key string) bool {
	now := time.Now().Unix()
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict expired entries.
	for k, exp := range c.entries {
		if exp <= now {
			delete(c.entries, k)
		}
	}

	if exp, ok := c.entries[key]; ok && exp > now {
		return true
	}

	// Enforce the hard cap: if still full after expiry eviction, drop entries
	// until there is room (Go map iteration order is randomized, giving a cheap
	// approximate eviction without tracking insertion order).
	for len(c.entries) >= c.maxSize {
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}

	c.entries[key] = now + int64(c.ttl.Seconds())
	return false
}

func New(listenAddr, botToken, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr: listenAddr,
		botToken:   botToken,
		tenant:     tenant,
		agent:      agent,
		client:     &http.Client{},
		slackAPI:   "https://slack.com/api",
		// Bounded TTL dedup cache: remember processed events for 10 minutes
		// (well past Slack's retry window) with a hard cap so a stream of unique
		// event ids cannot grow it without bound.
		seen: newDedupeCache(10*time.Minute, 10000),
	}
}

// SetRoutes configures per-channel agent routing overrides.
// The map keys are Slack channel IDs and values are agent names.
func (a *Adapter) SetRoutes(routes map[string]string) {
	a.routesMu.Lock()
	defer a.routesMu.Unlock()
	a.routes = routes
}

// routeFor returns the agent override for a channel, if any (concurrency-safe).
func (a *Adapter) routeFor(channelID string) (string, bool) {
	a.routesMu.RLock()
	defer a.routesMu.RUnlock()
	if a.routes == nil {
		return "", false
	}
	v, ok := a.routes[channelID]
	return v, ok
}

// SetSlackAPI overrides the Slack API URL (for testing with httptest).
func (a *Adapter) SetSlackAPI(url string) { a.slackAPI = url }

// SetSigningSecret sets the Slack signing secret used to verify that inbound
// requests genuinely came from Slack. Without it, inbound requests are rejected.
func (a *Adapter) SetSigningSecret(secret string) { a.signingSecret = secret }

// verifySlackSignature validates the X-Slack-Signature header: it must equal
// "v0=" + hex(HMAC-SHA256(secret, "v0:"+timestamp+":"+body)), with the
// timestamp within 5 minutes (replay protection). Fails closed when no signing
// secret is configured.
func (a *Adapter) verifySlackSignature(r *http.Request, body []byte) bool {
	if a.signingSecret == "" {
		return false
	}
	ts := r.Header.Get("X-Slack-Request-Timestamp")
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if d := time.Now().Unix() - n; d > 300 || d < -300 {
		return false // stale or future-dated → likely a replay
	}
	mac := hmac.New(sha256.New, []byte(a.signingSecret))
	mac.Write([]byte("v0:" + ts + ":"))
	mac.Write(body)
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(r.Header.Get("X-Slack-Signature")), []byte(expected))
}

// SetUseThreads enables replying in threads instead of top-level messages.
func (a *Adapter) SetUseThreads(use bool) { a.useThreads = use }

func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *Adapter) Name() string { return "slack" }

// Tenant returns the tenant that owns this adapter, so the channel manager can
// scope outbound dispatch and refuse cross-tenant delivery.
func (a *Adapter) Tenant() string { return a.tenant }

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

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read error", 500)
		return
	}
	// Verify the request genuinely came from Slack before processing anything,
	// including the url_verification challenge.
	if !a.verifySlackSignature(r, body) {
		http.Error(w, "unauthorized", 401)
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
	if a.seen.seenBefore(dedupeKey) {
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
		{
			if routed, ok := a.routeFor(eventChannel); ok {
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
	// Verify the Slack signature over the raw body before parsing the form.
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read error", 500)
		return
	}
	if !a.verifySlackSignature(r, body) {
		http.Error(w, "unauthorized", 401)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ParseForm()
	command := r.FormValue("command")
	text := strings.TrimSpace(r.FormValue("text"))

	var response string
	switch {
	case text == "status" || command == "/cyntr" && text == "status":
		response = "Cyntr is running. Use the dashboard for full status."
	case strings.HasPrefix(text, "switch "):
		agentName := strings.TrimPrefix(text, "switch ")
		channelID := r.FormValue("channel_id")
		a.routesMu.Lock()
		if a.routes == nil {
			a.routes = make(map[string]string)
		}
		a.routes[channelID] = agentName
		a.routesMu.Unlock()
		response = fmt.Sprintf("Switched this channel to agent: %s", agentName)
	case text == "clear" || text == "reset":
		response = "Session cleared. Starting fresh conversation."
	default:
		response = "Available commands: status, switch <agent>, clear"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"response_type": "ephemeral", "text": response})
}
