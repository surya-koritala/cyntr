package googlechat

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

var logger = log.Default().WithModule("channel_googlechat")

// googleChatCertURL is Google's X.509 public-cert endpoint for the system
// service account that signs Chat bot request JWTs.
const googleChatCertURL = "https://www.googleapis.com/service_accounts/v1/metadata/x509/chat@system.gserviceaccount.com"

// googleChatIssuer is the expected JWT issuer for Google Chat requests.
const googleChatIssuer = "chat@system.gserviceaccount.com"

// maxInboundBody bounds inbound webhook bodies to mitigate DoS.
const maxInboundBody = 1 << 20 // 1 MiB

type Adapter struct {
	listenAddr string
	webhookURL string
	tenant     string
	agent      string
	audience   string // expected JWT "aud" (Chat project number); empty = reject inbound
	certURL    string // overridable Google cert endpoint (for tests)
	handler    channel.InboundHandler
	listener   net.Listener
	server     *http.Server
	client     *http.Client
	seen       sync.Map

	certMu    sync.Mutex
	certCache map[string]*rsa.PublicKey
	certExp   time.Time
}

func New(listenAddr, webhookURL, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr: listenAddr,
		webhookURL: webhookURL,
		tenant:     tenant,
		agent:      agent,
		client:     &http.Client{Timeout: 10 * time.Second},
		certURL:    googleChatCertURL,
	}
}

// SetAudience configures the expected JWT audience (the Chat app's project
// number). When unset, all inbound requests are rejected (fail closed).
func (a *Adapter) SetAudience(aud string) { a.audience = aud }

// SetCertURL overrides Google's public-cert endpoint (for tests).
func (a *Adapter) SetCertURL(url string) { a.certURL = url }

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

// verifyBearer validates the "Authorization: Bearer <jwt>" header. The JWT must
// be an RS256 token signed by Google's Chat service account, with a matching
// issuer, the configured audience, and a non-expired window. Fails closed when
// no audience is configured.
func (a *Adapter) verifyBearer(r *http.Request) error {
	if a.audience == "" {
		return fmt.Errorf("no audience configured")
	}
	authz := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(authz) <= len(prefix) || authz[:len(prefix)] != prefix {
		return fmt.Errorf("missing bearer token")
	}
	token := authz[len(prefix):]

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("malformed jwt")
	}

	var hdr struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	hdrJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("decode header: %w", err)
	}
	if err := json.Unmarshal(hdrJSON, &hdr); err != nil {
		return fmt.Errorf("parse header: %w", err)
	}
	if hdr.Alg != "RS256" {
		return fmt.Errorf("unexpected alg %q", hdr.Alg)
	}

	keys, err := a.googleCerts()
	if err != nil {
		return fmt.Errorf("load certs: %w", err)
	}
	pub, ok := keys[hdr.Kid]
	if !ok {
		return fmt.Errorf("unknown key id")
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig); err != nil {
		return fmt.Errorf("signature: %w", err)
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("decode claims: %w", err)
	}
	var claims struct {
		Iss string `json:"iss"`
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return fmt.Errorf("parse claims: %w", err)
	}
	if claims.Iss != googleChatIssuer {
		return fmt.Errorf("unexpected issuer %q", claims.Iss)
	}
	if claims.Aud != a.audience {
		return fmt.Errorf("audience mismatch")
	}
	if claims.Exp == 0 || time.Now().Unix() > claims.Exp {
		return fmt.Errorf("token expired")
	}
	return nil
}

// googleCerts returns Google's current RS256 public keys keyed by kid, caching
// them until the HTTP response's freshness window elapses (or 1 hour).
func (a *Adapter) googleCerts() (map[string]*rsa.PublicKey, error) {
	a.certMu.Lock()
	defer a.certMu.Unlock()
	if a.certCache != nil && time.Now().Before(a.certExp) {
		return a.certCache, nil
	}

	url := a.certURL
	if url == "" {
		url = googleChatCertURL
	}
	resp, err := a.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("cert endpoint status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxInboundBody))
	if err != nil {
		return nil, err
	}
	// The endpoint returns a JSON object of { kid: "<PEM cert>" }.
	var certs map[string]string
	if err := json.Unmarshal(raw, &certs); err != nil {
		return nil, fmt.Errorf("parse certs: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(certs))
	for kid, certPEM := range certs {
		block, _ := pem.Decode([]byte(certPEM))
		if block == nil {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		if rsaPub, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			keys[kid] = rsaPub
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no usable certs")
	}
	a.certCache = keys
	a.certExp = time.Now().Add(time.Hour)
	return keys, nil
}

func (a *Adapter) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	// Verify the Google-signed bearer JWT (fail closed if no audience set).
	if err := a.verifyBearer(r); err != nil {
		logger.Error("bearer verification failed", map[string]any{"error": err.Error()})
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxInboundBody)
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
			logger.Error("message handler failed", map[string]any{"error": err.Error()})
			return
		}

		a.Send(context.Background(), channel.OutboundMessage{
			Channel:   "googlechat",
			ChannelID: spaceID,
			Text:      response,
		})
	}()
}
