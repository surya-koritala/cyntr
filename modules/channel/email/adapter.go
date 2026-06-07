package email

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

// maxInboundBody bounds inbound webhook bodies to mitigate DoS.
const maxInboundBody = 1 << 20 // 1 MiB

type Adapter struct {
	listenAddr string
	smtpHost   string
	smtpPort   string
	fromAddr   string
	tenant     string
	agent      string
	authToken  string // shared secret for inbound webhook; empty = reject all inbound
	handler    channel.InboundHandler
	listener   net.Listener
	server     *http.Server
	sendFunc   func(addr string, from string, to []string, msg []byte) error // injectable for testing
}

func defaultSendMail(addr string, from string, to []string, msg []byte) error {
	return smtp.SendMail(addr, nil, from, to, msg)
}

func New(listenAddr, smtpHost, smtpPort, fromAddr, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr: listenAddr, smtpHost: smtpHost, smtpPort: smtpPort,
		fromAddr: fromAddr, tenant: tenant, agent: agent,
		sendFunc: defaultSendMail,
	}
}

func (a *Adapter) SetSendFunc(fn func(string, string, []string, []byte) error) { a.sendFunc = fn }

// SetAuthToken configures the shared secret expected on inbound webhook
// requests (Authorization: Bearer <token>, or X-Auth-Token). When unset, all
// inbound requests are rejected (fail closed).
func (a *Adapter) SetAuthToken(token string) { a.authToken = token }
func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *Adapter) Name() string { return "email" }

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/email/inbound", a.handleInbound)

	ln, err := net.Listen("tcp", a.listenAddr)
	if err != nil {
		return fmt.Errorf("email listen: %w", err)
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
	to := msg.ChannelID // ChannelID is the recipient email
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Cyntr Agent Response\r\n\r\n%s", a.fromAddr, to, msg.Text)

	addr := a.smtpHost + ":" + a.smtpPort
	return a.sendFunc(addr, a.fromAddr, []string{to}, []byte(body))
}

type inboundEmail struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// authenticate verifies the inbound shared secret in constant time. It accepts
// the token either as "Authorization: Bearer <token>" or "X-Auth-Token". When
// no token is configured the request is rejected (fail closed).
func (a *Adapter) authenticate(r *http.Request) bool {
	if a.authToken == "" {
		return false
	}
	presented := r.Header.Get("X-Auth-Token")
	if presented == "" {
		const prefix = "Bearer "
		if h := r.Header.Get("Authorization"); len(h) > len(prefix) && h[:len(prefix)] == prefix {
			presented = h[len(prefix):]
		}
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(a.authToken)) == 1
}

func (a *Adapter) handleInbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}

	// Authenticate the caller via a shared secret (fail closed if unset). The
	// "from" field in the body is treated as routing data only, never identity.
	if !a.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxInboundBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read error", 500)
		return
	}

	var email inboundEmail
	if err := json.Unmarshal(body, &email); err != nil {
		http.Error(w, "invalid JSON", 400)
		return
	}

	response, err := a.handler(channel.InboundMessage{
		Channel: "email", ChannelID: email.From, UserID: email.From,
		Text: email.Body, Tenant: a.tenant, Agent: a.agent,
	})
	if err != nil {
		w.WriteHeader(200)
		return
	}

	// Reply via email
	a.Send(context.Background(), channel.OutboundMessage{
		Channel: "email", ChannelID: email.From, Text: response,
	})

	w.WriteHeader(200)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
