package sms

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

// twilioSignature computes the X-Twilio-Signature for a webhook POST: it is
// base64(HMAC-SHA1(authToken, fullURL + concatenation of sorted POST params as
// key+value)).
func twilioSignature(authToken, fullURL string, form url.Values) string {
	keys := make([]string, 0, len(form))
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(fullURL)
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(form.Get(k))
	}
	mac := hmac.New(sha1.New, []byte(authToken))
	mac.Write([]byte(sb.String()))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestSMSAdapterInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestSMSAdapterName(t *testing.T) {
	if New("", "", "", "", "", "").Name() != "sms" {
		t.Fatal("expected sms")
	}
}

func TestSMSSend(t *testing.T) {
	var gotPath string
	var gotForm url.Values
	var gotUser, gotPass string
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		gotUser, gotPass, _ = r.BasicAuth()
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		w.WriteHeader(201)
		w.Write([]byte(`{"sid":"SMabcdef"}`))
	}))
	defer server.Close()

	a := New("127.0.0.1:0", "ACtestSID", "secret-token", "+15555550199", "demo", "assistant")
	a.SetAPIBase(server.URL)
	if err := a.Send(context.Background(), channel.OutboundMessage{ChannelID: "+15555550100", Text: "hello world"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotPath != "/2010-04-01/Accounts/ACtestSID/Messages.json" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if gotContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("unexpected content type %q", gotContentType)
	}
	if gotUser != "ACtestSID" || gotPass != "secret-token" {
		t.Fatalf("unexpected basic auth %q/%q", gotUser, gotPass)
	}
	if gotForm.Get("From") != "+15555550199" {
		t.Fatalf("unexpected From %q", gotForm.Get("From"))
	}
	if gotForm.Get("To") != "+15555550100" {
		t.Fatalf("unexpected To %q", gotForm.Get("To"))
	}
	if gotForm.Get("Body") != "hello world" {
		t.Fatalf("unexpected Body %q", gotForm.Get("Body"))
	}
}

func TestSMSInbound(t *testing.T) {
	const authToken = "token"
	received := make(chan channel.InboundMessage, 1)
	a := New("127.0.0.1:0", "AC", authToken, "+15555550199", "tenantA", "assistant")
	ctx := context.Background()
	if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "reply!", nil
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.Stop(ctx)
	time.Sleep(50 * time.Millisecond)

	// Pin the public URL Twilio signs against so the signature is deterministic
	// regardless of the ephemeral listen address.
	publicURL := "http://" + a.Addr() + "/sms/inbound"
	a.SetPublicURL(publicURL)

	form := url.Values{}
	form.Set("From", "+15555550100")
	form.Set("To", "+15555550199")
	form.Set("Body", "ping")

	req, err := http.NewRequest("POST", publicURL, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Twilio-Signature", twilioSignature(authToken, publicURL, form))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(respBody), "<Message>reply!</Message>") {
		t.Fatalf("expected TwiML reply, got %q", string(respBody))
	}

	select {
	case msg := <-received:
		if msg.Text != "ping" {
			t.Fatalf("expected ping, got %q", msg.Text)
		}
		if msg.ChannelID != "+15555550100" {
			t.Fatalf("expected From as ChannelID, got %q", msg.ChannelID)
		}
		if msg.Tenant != "tenantA" {
			t.Fatalf("expected tenantA, got %q", msg.Tenant)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound message")
	}
}

func TestSMSInboundRejectsBadSignature(t *testing.T) {
	a := New("127.0.0.1:0", "AC", "token", "+15555550199", "tenantA", "assistant")
	ctx := context.Background()
	if err := a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		t.Error("handler must not be called for an invalid signature")
		return "reply!", nil
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer a.Stop(ctx)
	time.Sleep(50 * time.Millisecond)

	publicURL := "http://" + a.Addr() + "/sms/inbound"
	a.SetPublicURL(publicURL)

	form := url.Values{}
	form.Set("From", "+15555550100")
	form.Set("To", "+15555550199")
	form.Set("Body", "ping")

	cases := []struct {
		name      string
		signature string
	}{
		{"missing signature", ""},
		{"wrong signature", "not-a-valid-signature"},
		{"tampered signature", twilioSignature("wrong-token", publicURL, form)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", publicURL, strings.NewReader(form.Encode()))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if tc.signature != "" {
				req.Header.Set("X-Twilio-Signature", tc.signature)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", resp.StatusCode)
			}
		})
	}
}

func TestSMSSendNoCreds(t *testing.T) {
	a := New("127.0.0.1:0", "", "", "+1", "t", "a")
	err := a.Send(context.Background(), channel.OutboundMessage{ChannelID: "+15555550100", Text: "x"})
	if err == nil {
		t.Fatal("expected error with missing creds")
	}
}
