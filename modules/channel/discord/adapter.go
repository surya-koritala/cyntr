package discord

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
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
	publicKey  ed25519.PublicKey // Discord application public key for verification
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

// SetPublicKey sets the Discord application public key (hex) used to verify the
// Ed25519 signature on inbound interactions. Without it, interactions are
// rejected (fail closed).
func (a *Adapter) SetPublicKey(hexKey string) error {
	raw, err := hex.DecodeString(hexKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return fmt.Errorf("discord: invalid public key")
	}
	a.publicKey = ed25519.PublicKey(raw)
	return nil
}

// verifyInteraction checks the Ed25519 signature over (timestamp + body) using
// the X-Signature-Ed25519 / X-Signature-Timestamp headers. Fails closed when no
// public key is configured.
func (a *Adapter) verifyInteraction(r *http.Request, body []byte) bool {
	if len(a.publicKey) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(r.Header.Get("X-Signature-Ed25519"))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	ts := r.Header.Get("X-Signature-Timestamp")
	if ts == "" {
		return false
	}
	return ed25519.Verify(a.publicKey, append([]byte(ts), body...), sig)
}
func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}
func (a *Adapter) Name() string { return "discord" }

// Tenant returns the tenant that owns this adapter, so the channel manager can
// scope outbound dispatch and refuse cross-tenant delivery.
func (a *Adapter) Tenant() string { return a.tenant }

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

	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	// Verify the Ed25519 signature before trusting any interaction, including
	// the PING handshake.
	if !a.verifyInteraction(r, body) {
		http.Error(w, "invalid request signature", 401)
		return
	}
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
