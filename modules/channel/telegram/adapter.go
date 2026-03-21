package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

var logger = log.Default().WithModule("channel_telegram")

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
		client: &http.Client{}, apiURL: "https://api.telegram.org",
	}
}

func (a *Adapter) SetAPIURL(url string) { a.apiURL = url }
func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}
func (a *Adapter) Name() string { return "telegram" }

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/telegram/webhook", a.handleUpdate)
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
	payload := map[string]any{"chat_id": msg.ChannelID, "text": msg.Text}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("%s/bot%s/sendMessage", a.apiURL, a.botToken)
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram error %d: %s", resp.StatusCode, b)
	}
	return nil
}

func (a *Adapter) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	w.WriteHeader(200)

	body, _ := io.ReadAll(r.Body)
	var update struct {
		Message struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
			From struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
			} `json:"from"`
			Text string `json:"text"`
		} `json:"message"`
	}
	if json.Unmarshal(body, &update) != nil || update.Message.Text == "" {
		return
	}

	chatID := strconv.FormatInt(update.Message.Chat.ID, 10)
	userID := strconv.FormatInt(update.Message.From.ID, 10)
	text := update.Message.Text

	go func() {
		response, err := a.handler(channel.InboundMessage{
			Channel: "telegram", ChannelID: chatID, UserID: userID,
			Text: text, Tenant: a.tenant, Agent: a.agent,
		})
		if err != nil {
			logger.Error("message handler failed", map[string]any{"error": err.Error()})
			return
		}
		a.Send(context.Background(), channel.OutboundMessage{Channel: "telegram", ChannelID: chatID, Text: response})
	}()
}
