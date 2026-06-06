package whatsapp

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
	"time"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

var logger = log.Default().WithModule("channel_whatsapp")

// maxInboundBody bounds inbound webhook bodies to mitigate DoS.
const maxInboundBody = 1 << 20 // 1 MiB

// maxConcurrentHandlers bounds the number of in-flight inbound message
// handlers, preventing unbounded goroutine growth under load.
const maxConcurrentHandlers = 32

// handlerTimeout bounds how long a single inbound message handler may run.
const handlerTimeout = 30 * time.Second

type Adapter struct {
	listenAddr  string
	accessToken string
	phoneNumID  string
	verifyToken string
	appSecret   string // Meta app secret for X-Hub-Signature-256; empty = reject inbound POSTs
	tenant      string
	agent       string
	handler     channel.InboundHandler
	listener    net.Listener
	server      *http.Server
	client      *http.Client
	apiURL      string
	sem         chan struct{}
}

func New(listenAddr, accessToken, phoneNumID, verifyToken, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr: listenAddr, accessToken: accessToken, phoneNumID: phoneNumID,
		verifyToken: verifyToken, tenant: tenant, agent: agent,
		client: &http.Client{}, apiURL: "https://graph.facebook.com/v17.0",
		sem: make(chan struct{}, maxConcurrentHandlers),
	}
}

// SetAppSecret configures the Meta app secret used to verify the
// X-Hub-Signature-256 header on inbound webhook POSTs. When unset, all inbound
// POSTs are rejected (fail closed).
func (a *Adapter) SetAppSecret(secret string) { a.appSecret = secret }

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

// verifySignature validates the X-Hub-Signature-256 header, which Meta computes
// as "sha256=" + hex(HMAC-SHA256(appSecret, rawBody)). Comparison is constant
// time via hmac.Equal. Fails closed when no app secret is configured.
func (a *Adapter) verifySignature(r *http.Request, body []byte) bool {
	if a.appSecret == "" {
		return false
	}
	header := r.Header.Get("X-Hub-Signature-256")
	const prefix = "sha256="
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return false
	}
	provided, err := hex.DecodeString(header[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(a.appSecret))
	mac.Write(body)
	expected := mac.Sum(nil)
	return hmac.Equal(provided, expected)
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

	body, err := io.ReadAll(io.LimitReader(r.Body, maxInboundBody))
	if err != nil {
		http.Error(w, "read error", 500)
		return
	}

	// Verify the X-Hub-Signature-256 HMAC over the raw body (fail closed).
	if !a.verifySignature(r, body) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	w.WriteHeader(200) // ACK after authentication

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
				// Bound concurrency: acquire a slot or drop the message rather
				// than spawning unbounded goroutines under load.
				select {
				case a.sem <- struct{}{}:
				default:
					logger.Error("handler pool saturated, dropping message", map[string]any{"from": msg.From})
					continue
				}
				go func(from, text string) {
					defer func() { <-a.sem }()
					ctx, cancel := context.WithTimeout(context.Background(), handlerTimeout)
					defer cancel()
					response, err := a.handler(channel.InboundMessage{
						Channel: "whatsapp", ChannelID: from, UserID: from,
						Text: text, Tenant: a.tenant, Agent: a.agent,
					})
					if err != nil {
						logger.Error("message handler failed", map[string]any{"error": err.Error()})
						return
					}
					a.Send(ctx, channel.OutboundMessage{Channel: "whatsapp", ChannelID: from, Text: response})
				}(msg.From, msg.Text.Body)
			}
		}
	}
}
