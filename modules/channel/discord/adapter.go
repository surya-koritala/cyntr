package discord

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

var logger = log.Default().WithModule("channel_discord")

type Adapter struct {
	listenAddr string
	botToken   string
	tenant     string
	agent      string
	handler    channel.InboundHandler
	listener   net.Listener
	server     *http.Server
	client     *http.Client
	apiURL     string
}

func New(listenAddr, botToken, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr: listenAddr, botToken: botToken, tenant: tenant, agent: agent,
		client: &http.Client{}, apiURL: "https://discord.com/api/v10",
	}
}

func (a *Adapter) SetAPIURL(url string) { a.apiURL = url }
func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}
func (a *Adapter) Name() string { return "discord" }

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/discord/interactions", a.handleInteraction)
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
	payload := map[string]string{"content": msg.Text}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/channels/%s/messages", a.apiURL, msg.ChannelID)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bot "+a.botToken)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord error %d: %s", resp.StatusCode, b)
	}
	return nil
}

func (a *Adapter) handleInteraction(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	body, _ := io.ReadAll(r.Body)
	var interaction struct {
		Type int `json:"type"`
		Data struct {
			Name string `json:"name"`
		} `json:"data"`
		ChannelID string `json:"channel_id"`
		Member    struct {
			User struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			} `json:"user"`
		} `json:"member"`
		// For message components
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(body, &interaction) != nil {
		http.Error(w, "bad json", 400)
		return
	}

	// Type 1 = PING (verification)
	if interaction.Type == 1 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"type": 1})
		return
	}

	w.WriteHeader(200)

	// Type 2 = APPLICATION_COMMAND
	if interaction.Type != 2 {
		return
	}

	text := interaction.Data.Name
	channelID := interaction.ChannelID
	userID := interaction.Member.User.ID

	go func() {
		response, err := a.handler(channel.InboundMessage{
			Channel: "discord", ChannelID: channelID, UserID: userID,
			Text: text, Tenant: a.tenant, Agent: a.agent,
		})
		if err != nil {
			logger.Error("message handler failed", map[string]any{"error": err.Error()})
			return
		}
		a.Send(context.Background(), channel.OutboundMessage{Channel: "discord", ChannelID: channelID, Text: response})
	}()
}
