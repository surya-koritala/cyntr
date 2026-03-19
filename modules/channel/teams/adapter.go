package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

type Adapter struct {
	listenAddr string
	appID      string
	appSecret  string
	tenant     string
	agent      string
	handler    channel.InboundHandler
	listener   net.Listener
	server     *http.Server
	client     *http.Client
	serviceURL string // override for testing
}

func New(listenAddr, appID, appSecret, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr: listenAddr, appID: appID, appSecret: appSecret,
		tenant: tenant, agent: agent, client: &http.Client{},
	}
}

func (a *Adapter) SetServiceURL(url string) { a.serviceURL = url }
func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *Adapter) Name() string { return "teams" }

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/teams/messages", a.handleActivity)

	ln, err := net.Listen("tcp", a.listenAddr)
	if err != nil {
		return fmt.Errorf("teams listen: %w", err)
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
	url := a.serviceURL
	if url == "" {
		return fmt.Errorf("no service URL configured")
	}

	payload := map[string]string{"type": "message", "text": msg.Text}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("teams send: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

type teamsActivity struct {
	Type         string `json:"type"`
	Text         string `json:"text"`
	ServiceURL   string `json:"serviceUrl"`
	From         struct {
		ID string `json:"id"`
	} `json:"from"`
	Conversation struct {
		ID string `json:"id"`
	} `json:"conversation"`
}

func (a *Adapter) handleActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", 500)
		return
	}

	var activity teamsActivity
	if err := json.Unmarshal(body, &activity); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}

	if activity.Type != "message" {
		w.WriteHeader(200)
		return
	}

	response, err := a.handler(channel.InboundMessage{
		Channel: "teams", ChannelID: activity.Conversation.ID,
		UserID: activity.From.ID, Text: activity.Text,
		Tenant: a.tenant, Agent: a.agent,
	})
	if err != nil {
		w.WriteHeader(200)
		return
	}

	// Reply
	replyURL := activity.ServiceURL
	if a.serviceURL != "" {
		replyURL = a.serviceURL
	}
	if replyURL != "" {
		payload := map[string]string{"type": "message", "text": response}
		b, _ := json.Marshal(payload)
		http.Post(replyURL, "application/json", bytes.NewReader(b))
	}

	w.WriteHeader(200)
}
