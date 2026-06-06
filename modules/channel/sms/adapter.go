package sms

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
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
	publicURL  string // externally visible inbound URL Twilio signs against; empty = derive from request
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

// SetPublicURL sets the externally visible URL of the inbound webhook that
// Twilio signs requests against (e.g. "https://example.com/sms/inbound"). When
// unset, the URL is reconstructed from the incoming request, which may be wrong
// behind TLS-terminating proxies; setting it explicitly is recommended.
func (a *Adapter) SetPublicURL(u string) { a.publicURL = u }

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

	// Validate the Twilio request signature (fail closed if no auth token).
	if !a.validateTwilioSignature(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
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

// validateTwilioSignature verifies the X-Twilio-Signature header. Twilio
// computes it as base64(HMAC-SHA1(authToken, fullURL + concatenation of
// sorted POST params as key+value)). Fails closed when no auth token is set.
func (a *Adapter) validateTwilioSignature(r *http.Request) bool {
	if a.authToken == "" {
		return false
	}
	provided := r.Header.Get("X-Twilio-Signature")
	if provided == "" {
		return false
	}

	fullURL := a.publicURL
	if fullURL == "" {
		scheme := "https"
		if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
			scheme = "http"
		}
		fullURL = scheme + "://" + r.Host + r.URL.RequestURI()
	}

	// Build the signing string: URL followed by each POST param key+value in
	// lexicographic key order.
	keys := make([]string, 0, len(r.PostForm))
	for k := range r.PostForm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(fullURL)
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(r.PostForm.Get(k))
	}

	mac := hmac.New(sha1.New, []byte(a.authToken))
	mac.Write([]byte(sb.String()))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(provided), []byte(expected))
}

func twimlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
