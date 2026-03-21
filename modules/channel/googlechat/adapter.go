package googlechat

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
	webhookURL string
	tenant     string
	agent      string
	handler    channel.InboundHandler
	listener   net.Listener
	server     *http.Server
	client     *http.Client
	seen       sync.Map
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

func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *Adapter) Name() string { return "googlechat" }

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/googlechat/events", a.handleEvents)

	ln, err := net.Listen("tcp", a.listenAddr)
	if err != nil {
		return fmt.Errorf("googlechat listen: %w", err)
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
	payload := map[string]string{"text": msg.Text}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", a.webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("googlechat send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("googlechat API error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

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

	var event struct {
		Type    string `json:"type"`
		EventID string `json:"eventId"`
		Message struct {
			Name   string `json:"name"`
			Sender struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
				Type        string `json:"type"`
			} `json:"sender"`
			Text string `json:"text"`
		} `json:"message"`
		Space struct {
			Name string `json:"name"`
		} `json:"space"`
	}

	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}

	// Only handle MESSAGE events
	if event.Type != "MESSAGE" {
		w.WriteHeader(200)
		return
	}

	// Skip BOT messages to avoid loops
	if event.Message.Sender.Type == "BOT" {
		w.WriteHeader(200)
		return
	}

	// Deduplicate using message name
	dedupeKey := event.Message.Name
	if dedupeKey == "" {
		dedupeKey = event.EventID
	}
	if dedupeKey == "" {
		dedupeKey = event.Message.Sender.Name + ":" + event.Space.Name + ":" + event.Message.Text
	}
	if _, loaded := a.seen.LoadOrStore(dedupeKey, true); loaded {
		w.WriteHeader(200)
		return
	}

	// ACK immediately, process async
	w.WriteHeader(200)

	spaceID := event.Space.Name
	userID := event.Message.Sender.Name
	text := event.Message.Text

	go func() {
		response, err := a.handler(channel.InboundMessage{
			Channel:   "googlechat",
			ChannelID: spaceID,
			UserID:    userID,
			Text:      text,
			Tenant:    a.tenant,
			Agent:     a.agent,
		})

		if err != nil {
			return
		}

		a.Send(context.Background(), channel.OutboundMessage{
			Channel:   "googlechat",
			ChannelID: spaceID,
			Text:      response,
		})
	}()
}
