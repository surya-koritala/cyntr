package whatsapp

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
	listenAddr  string
	accessToken string
	phoneNumID  string
	verifyToken string
	tenant      string
	agent       string
	handler     channel.InboundHandler
	listener    net.Listener
	server      *http.Server
	client      *http.Client
	apiURL      string
}

func New(listenAddr, accessToken, phoneNumID, verifyToken, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr: listenAddr, accessToken: accessToken, phoneNumID: phoneNumID,
		verifyToken: verifyToken, tenant: tenant, agent: agent,
		client: &http.Client{}, apiURL: "https://graph.facebook.com/v17.0",
	}
}

func (a *Adapter) SetAPIURL(url string) { a.apiURL = url }
func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}
func (a *Adapter) Name() string { return "whatsapp" }

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/whatsapp/webhook", a.handleWebhook)
	ln, err := net.Listen("tcp", a.listenAddr)
	if err != nil {
		return err
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
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                msg.ChannelID,
		"type":              "text",
		"text":              map[string]string{"body": msg.Text},
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/%s/messages", a.apiURL, a.phoneNumID)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.accessToken)
	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("whatsapp error %d: %s", resp.StatusCode, b)
	}
	return nil
}

func (a *Adapter) handleWebhook(w http.ResponseWriter, r *http.Request) {
	// GET = verification challenge
	if r.Method == "GET" {
		mode := r.URL.Query().Get("hub.mode")
		token := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")
		if mode == "subscribe" && token == a.verifyToken {
			w.WriteHeader(200)
			fmt.Fprint(w, challenge)
			return
		}
		w.WriteHeader(403)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	w.WriteHeader(200) // ACK immediately

	body, _ := io.ReadAll(r.Body)
	var payload struct {
		Entry []struct {
			Changes []struct {
				Value struct {
					Messages []struct {
						From string `json:"from"`
						Text struct {
							Body string `json:"body"`
						} `json:"text"`
						Type string `json:"type"`
					} `json:"messages"`
				} `json:"value"`
			} `json:"changes"`
		} `json:"entry"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			for _, msg := range change.Value.Messages {
				if msg.Type != "text" {
					continue
				}
				go func(from, text string) {
					response, err := a.handler(channel.InboundMessage{
						Channel: "whatsapp", ChannelID: from, UserID: from,
						Text: text, Tenant: a.tenant, Agent: a.agent,
					})
					if err != nil {
						return
					}
					a.Send(context.Background(), channel.OutboundMessage{Channel: "whatsapp", ChannelID: from, Text: response})
				}(msg.From, msg.Text.Body)
			}
		}
	}
}
