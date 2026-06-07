package googlechat

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

const testAudience = "test-project-number"

// testAuth holds a freshly generated signing key and a fake Google cert
// endpoint that serves the matching public cert. It is used to mint valid
// Google-Chat-style RS256 bearer JWTs for the happy-path tests.
type testAuth struct {
	key     *rsa.PrivateKey
	kid     string
	certSrv *httptest.Server
}

func newTestAuth(t *testing.T) *testAuth {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	kid := "test-kid"

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: googleChatIssuer},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	certSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{kid: string(certPEM)})
	}))

	auth := &testAuth{key: key, kid: kid, certSrv: certSrv}
	t.Cleanup(certSrv.Close)
	return auth
}

// configure wires the adapter to trust this test auth (audience + cert URL).
func (ta *testAuth) configure(a *Adapter) {
	a.SetAudience(testAudience)
	a.SetCertURL(ta.certSrv.URL)
}

// token mints a valid RS256 JWT for the configured audience and issuer.
func (ta *testAuth) token(t *testing.T) string {
	t.Helper()
	header := map[string]string{"alg": "RS256", "kid": ta.kid, "typ": "JWT"}
	claims := map[string]any{
		"iss": googleChatIssuer,
		"aud": testAudience,
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signingInput := enc(header) + "." + enc(claims)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, ta.key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// postEvent sends an events POST carrying a valid bearer token.
func postEvent(t *testing.T, addr string, ta *testAuth, body string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequest("POST", "http://"+addr+"/googlechat/events", strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ta.token(t))
	return http.DefaultClient.Do(req)
}

func TestGoogleChatAdapterImplementsInterface(t *testing.T) {
	var _ channel.ChannelAdapter = (*Adapter)(nil)
}

func TestGoogleChatAdapterName(t *testing.T) {
	a := New("127.0.0.1:0", "https://chat.googleapis.com/webhook", "marketing", "assistant")
	if a.Name() != "googlechat" {
		t.Fatalf("expected googlechat, got %q", a.Name())
	}
}

func TestGoogleChatAdapterReceivesMessage(t *testing.T) {
	received := make(chan channel.InboundMessage, 1)

	webhookAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer webhookAPI.Close()

	a := New("127.0.0.1:0", webhookAPI.URL, "marketing", "assistant")
	auth := newTestAuth(t)
	auth.configure(a)

	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		received <- msg
		return "Agent reply!", nil
	})
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"MESSAGE","eventId":"evt1","message":{"name":"spaces/123/messages/456","sender":{"name":"users/789","displayName":"Alice","type":"HUMAN"},"text":"Hello agent"},"space":{"name":"spaces/123"}}`
	resp, err := postEvent(t, a.Addr(), auth, body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()

	select {
	case msg := <-received:
		if msg.Text != "Hello agent" {
			t.Fatalf("expected message, got %q", msg.Text)
		}
		if msg.UserID != "users/789" {
			t.Fatalf("expected users/789, got %q", msg.UserID)
		}
		if msg.ChannelID != "spaces/123" {
			t.Fatalf("expected spaces/123, got %q", msg.ChannelID)
		}
		if msg.Tenant != "marketing" {
			t.Fatalf("expected marketing, got %q", msg.Tenant)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestGoogleChatAdapterSkipsBotMessages(t *testing.T) {
	handlerCalled := false
	a := New("127.0.0.1:0", "https://example.com", "t", "a")
	auth := newTestAuth(t)
	auth.configure(a)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		handlerCalled = true
		return "", nil
	})
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"MESSAGE","message":{"name":"msg1","sender":{"name":"users/bot","type":"BOT"},"text":"bot msg"},"space":{"name":"spaces/1"}}`
	resp, err := postEvent(t, a.Addr(), auth, body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)
	if handlerCalled {
		t.Fatal("handler should not be called for bot messages")
	}
}

func TestGoogleChatAdapterSkipsNonMessage(t *testing.T) {
	handlerCalled := false
	a := New("127.0.0.1:0", "https://example.com", "t", "a")
	auth := newTestAuth(t)
	auth.configure(a)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) {
		handlerCalled = true
		return "", nil
	})
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"ADDED_TO_SPACE","space":{"name":"spaces/1"}}`
	resp, err := postEvent(t, a.Addr(), auth, body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)
	if handlerCalled {
		t.Fatal("handler should not be called for non-message events")
	}
}

func TestGoogleChatAdapterSend(t *testing.T) {
	var sentPayload map[string]string
	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&sentPayload)
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer webhook.Close()

	a := New("127.0.0.1:0", webhook.URL, "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)

	err := a.Send(ctx, channel.OutboundMessage{ChannelID: "spaces/123", Text: "Hello from Cyntr"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if sentPayload["text"] != "Hello from Cyntr" {
		t.Fatalf("expected message, got %v", sentPayload)
	}
}

func TestGoogleChatAdapterBadJSON(t *testing.T) {
	a := New("127.0.0.1:0", "https://example.com", "t", "a")
	auth := newTestAuth(t)
	auth.configure(a)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, err := postEvent(t, a.Addr(), auth, "{bad")
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// TestGoogleChatAdapterRejectsForgedToken asserts that the adapter fails closed
// for unsigned/forged bearer tokens and for a missing Authorization header.
func TestGoogleChatAdapterRejectsForgedToken(t *testing.T) {
	a := New("127.0.0.1:0", "https://example.com", "t", "a")
	auth := newTestAuth(t)
	auth.configure(a)
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	body := `{"type":"MESSAGE","message":{"name":"m1","sender":{"name":"users/1","type":"HUMAN"},"text":"hi"},"space":{"name":"spaces/1"}}`

	// Forge a token: re-sign valid header/claims with an attacker key the
	// adapter does not trust.
	attacker, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen attacker key: %v", err)
	}
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signingInput := enc(map[string]string{"alg": "RS256", "kid": auth.kid, "typ": "JWT"}) + "." +
		enc(map[string]any{"iss": googleChatIssuer, "aud": testAudience, "exp": time.Now().Add(time.Hour).Unix()})
	digest := sha256.Sum256([]byte(signingInput))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, attacker, crypto.SHA256, digest[:])
	forged := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	cases := []struct {
		name string
		auth string
	}{
		{"missing header", ""},
		{"forged signature", "Bearer " + forged},
		{"garbage token", "Bearer not.a.jwt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", "http://"+a.Addr()+"/googlechat/events", strings.NewReader(body))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", resp.StatusCode)
			}
		})
	}
}

func TestGoogleChatAdapterMethodNotAllowed(t *testing.T) {
	a := New("127.0.0.1:0", "https://example.com", "t", "a")
	ctx := context.Background()
	a.Start(ctx, func(msg channel.InboundMessage) (string, error) { return "", nil })
	defer a.Stop(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, _ := http.Get("http://" + a.Addr() + "/googlechat/events")
	resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}
