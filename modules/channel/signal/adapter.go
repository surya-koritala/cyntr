package signal

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

var logger = log.Default().WithModule("channel_signal")

// Adapter integrates with a signal-cli JSON-RPC daemon for outbound send
// and exposes /inbound/signal for inbound delivery from a sidecar/poller.
type Adapter struct {
	listenAddr   string
	signalCLIURL string
	tenant       string
	agent        string
	handler      channel.InboundHandler
	listener     net.Listener
	server       *http.Server
	client       *http.Client
}

func New(listenAddr, signalCLIURL, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr:   listenAddr,
		signalCLIURL: signalCLIURL,
		tenant:       tenant,
		agent:        agent,
		client:       &http.Client{},
	}
}

// SetSignalCLIURL overrides the JSON-RPC endpoint (used in tests).
func (a *Adapter) SetSignalCLIURL(url string) { a.signalCLIURL = url }

func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *Adapter) Name() string { return "signal" }

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/inbound/signal", a.HandleInbound)

	ln, err := net.Listen("tcp", a.listenAddr)
	if err != nil {
		return fmt.Errorf("signal listen: %w", err)
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

type jsonRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
	ID      int            `json:"id"`
}

// Send issues a JSON-RPC send call to signal-cli.
func (a *Adapter) Send(ctx context.Context, msg channel.OutboundMessage) error {
	if a.signalCLIURL == "" {
		return fmt.Errorf("signal: no signal-cli URL configured")
	}
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  "send",
		Params: map[string]any{
			"recipient": msg.ChannelID,
			"message":   msg.Text,
		},
		ID: 1,
	}
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.signalCLIURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("signal send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("signal-cli error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// signalEnvelope matches the signal-cli JSON-RPC receive envelope (partial).
type signalEnvelope struct {
	Envelope struct {
		Source        string `json:"source"`
		SourceNumber  string `json:"sourceNumber"`
		DataMessage   struct {
			Message string `json:"message"`
		} `json:"dataMessage"`
	} `json:"envelope"`
}

// HandleInbound accepts a JSON envelope as produced by signal-cli's receive output.
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
	var env signalEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	text := env.Envelope.DataMessage.Message
	if text == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	source := env.Envelope.SourceNumber
	if source == "" {
		source = env.Envelope.Source
	}

	w.WriteHeader(http.StatusOK)

	go func() {
		response, err := a.handler(channel.InboundMessage{
			Channel:   "signal",
			ChannelID: source,
			UserID:    source,
			Text:      text,
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
			Channel:   "signal",
			ChannelID: source,
			Text:      response,
		})
	}()
}
