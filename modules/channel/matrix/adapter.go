package matrix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

var logger = log.Default().WithModule("channel_matrix")

// Adapter integrates with a Matrix homeserver via the Client-Server API.
// Outbound: PUT m.room.message events. Inbound: simplified appservice push
// at /inbound/matrix accepting a single event JSON.
type Adapter struct {
	listenAddr    string
	homeserverURL string
	accessToken   string
	tenant        string
	agent         string
	handler       channel.InboundHandler
	listener      net.Listener
	server        *http.Server
	client        *http.Client
	txnCounter    uint64
}

func New(listenAddr, homeserverURL, accessToken, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr:    listenAddr,
		homeserverURL: homeserverURL,
		accessToken:   accessToken,
		tenant:        tenant,
		agent:         agent,
		client:        &http.Client{},
	}
}

// SetHomeserverURL overrides the homeserver base URL (for tests).
func (a *Adapter) SetHomeserverURL(url string) { a.homeserverURL = url }

func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *Adapter) Name() string { return "matrix" }

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/inbound/matrix", a.HandleInbound)

	ln, err := net.Listen("tcp", a.listenAddr)
	if err != nil {
		return fmt.Errorf("matrix listen: %w", err)
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

// Send publishes an m.room.message event to the target room.
func (a *Adapter) Send(ctx context.Context, msg channel.OutboundMessage) error {
	if a.homeserverURL == "" || a.accessToken == "" {
		return fmt.Errorf("matrix: missing homeserver URL or access token")
	}
	if msg.ChannelID == "" {
		return fmt.Errorf("matrix: missing room ID")
	}
	txnID := strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.FormatUint(atomic.AddUint64(&a.txnCounter, 1), 10)
	endpoint := fmt.Sprintf("%s/_matrix/client/v3/rooms/%s/send/m.room.message/%s", a.homeserverURL, msg.ChannelID, txnID)
	payload := map[string]string{
		"msgtype": "m.text",
		"body":    msg.Text,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "PUT", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.accessToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("matrix send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("matrix API error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

type matrixEvent struct {
	Type    string `json:"type"`
	Sender  string `json:"sender"`
	RoomID  string `json:"room_id"`
	Content struct {
		Body    string `json:"body"`
		MsgType string `json:"msgtype"`
	} `json:"content"`
}

// HandleInbound accepts a single Matrix event JSON (simplified appservice push).
func (a *Adapter) HandleInbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", http.StatusInternalServerError)
		return
	}
	var ev matrixEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if ev.Type != "m.room.message" || ev.Content.Body == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)

	go func() {
		response, err := a.handler(channel.InboundMessage{
			Channel:   "matrix",
			ChannelID: ev.RoomID,
			UserID:    ev.Sender,
			Text:      ev.Content.Body,
			Tenant:    a.tenant,
			Agent:     a.agent,
		})
		if err != nil {
			logger.Error("message handler failed", map[string]any{"error": err.Error()})
			return
		}
		if response == "" {
			return
		}
		a.Send(context.Background(), channel.OutboundMessage{
			Channel:   "matrix",
			ChannelID: ev.RoomID,
			Text:      response,
		})
	}()
}
