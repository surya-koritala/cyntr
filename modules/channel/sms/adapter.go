package sms

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

var logger = log.Default().WithModule("channel_sms")

// Adapter is an SMS channel adapter implemented against Twilio's REST API.
type Adapter struct {
	listenAddr string
	accountSID string
	authToken  string
	fromNumber string
	tenant     string
	agent      string
	apiBase    string // overridable for tests
	handler    channel.InboundHandler
	listener   net.Listener
	server     *http.Server
	client     *http.Client
}

func New(listenAddr, accountSID, authToken, fromNumber, tenant, agent string) *Adapter {
	return &Adapter{
		listenAddr: listenAddr,
		accountSID: accountSID,
		authToken:  authToken,
		fromNumber: fromNumber,
		tenant:     tenant,
		agent:      agent,
		apiBase:    "https://api.twilio.com",
		client:     &http.Client{},
	}
}

// SetAPIBase overrides the Twilio API base URL (for tests).
func (a *Adapter) SetAPIBase(base string) { a.apiBase = base }

func (a *Adapter) Addr() string {
	if a.listener == nil {
		return ""
	}
	return a.listener.Addr().String()
}

func (a *Adapter) Name() string { return "sms" }

func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler
	mux := http.NewServeMux()
	mux.HandleFunc("/sms/inbound", a.HandleInbound)

	ln, err := net.Listen("tcp", a.listenAddr)
	if err != nil {
		return fmt.Errorf("sms listen: %w", err)
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

// Send delivers a message via Twilio's Messages API.
func (a *Adapter) Send(ctx context.Context, msg channel.OutboundMessage) error {
	if a.accountSID == "" || a.authToken == "" {
		return fmt.Errorf("sms: missing Twilio credentials")
	}
	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json", a.apiBase, a.accountSID)
	form := url.Values{}
	form.Set("From", a.fromNumber)
	form.Set("To", msg.ChannelID)
	form.Set("Body", msg.Text)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(a.accountSID, a.authToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("sms send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("twilio API error %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// HandleInbound accepts Twilio webhook form posts (From, To, Body).
func (a *Adapter) HandleInbound(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	from := r.FormValue("From")
	body := r.FormValue("Body")
	if body == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	response, err := a.handler(channel.InboundMessage{
		Channel:   "sms",
		ChannelID: from,
		UserID:    from,
		Text:      body,
		Tenant:    a.tenant,
		Agent:     a.agent,
	})
	if err != nil {
		logger.Error("message handler failed", map[string]any{"error": err.Error()})
		w.WriteHeader(http.StatusOK)
		return
	}

	// Reply via TwiML so Twilio sends the SMS back automatically.
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	if response != "" {
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Response><Message>%s</Message></Response>`, twimlEscape(response))
	} else {
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?><Response/>`)
	}
}

func twimlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
