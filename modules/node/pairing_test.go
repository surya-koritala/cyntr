package node

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newStore(t *testing.T) *PairingStore {
	t.Helper()
	s, err := NewPairingStore(filepath.Join(t.TempDir(), "node.db"))
	if err != nil {
		t.Fatalf("NewPairingStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// stubConn is an in-memory nodeConn driving the protocol without a network.
// in carries frames the client "sends"; out collects frames the server writes.
type stubConn struct {
	in     []Frame
	cursor int
	out    []Frame
	closed bool
}

func (c *stubConn) ReadFrame() (Frame, error) {
	if c.cursor >= len(c.in) {
		return Frame{}, errEOF
	}
	f := c.in[c.cursor]
	c.cursor++
	return f, nil
}
func (c *stubConn) WriteFrame(f Frame) error        { c.out = append(c.out, f); return nil }
func (c *stubConn) SetReadDeadline(time.Time) error { return nil }
func (c *stubConn) Close() error                    { c.closed = true; return nil }

type stringErr string

func (e stringErr) Error() string { return string(e) }

const errEOF = stringErr("eof")

func (c *stubConn) last() Frame {
	if len(c.out) == 0 {
		return Frame{}
	}
	return c.out[len(c.out)-1]
}

func (c *stubConn) firstOfType(t string) (Frame, bool) {
	for _, f := range c.out {
		if f.Type == t {
			return f, true
		}
	}
	return Frame{}, false
}

func TestStoreRedeemConsumesCode(t *testing.T) {
	s := newStore(t)
	code, err := s.IssueCode("acme", "phone-1", []string{CapVoice, CapCanvas})
	if err != nil || code == "" {
		t.Fatalf("issue: %q %v", code, err)
	}
	p, err := s.Redeem(code)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if p.Tenant != "acme" || p.Node != "phone-1" {
		t.Fatalf("bound wrong identity: %+v", p)
	}
	// Single-use: a second redeem of the same code fails closed.
	if _, err := s.Redeem(code); err == nil {
		t.Fatal("code must be single-use")
	}
	// But the node remains paired for reconnects.
	caps, ok := s.AllowedCaps("acme", "phone-1")
	if !ok || len(caps) != 2 {
		t.Fatalf("paired node caps: %v ok=%v", caps, ok)
	}
}

func TestStoreRedeemRejectsBadCodes(t *testing.T) {
	s := newStore(t)
	for _, code := range []string{"", "   ", "ZZZZZZ"} {
		if _, err := s.Redeem(code); err == nil {
			t.Fatalf("redeem(%q) should fail closed", code)
		}
	}
}

func TestNegotiateCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		offered  []string
		allowed  []string
		expected []string
	}{
		{"intersection", []string{CapVoice, CapCamera}, []string{CapVoice, CapCanvas}, []string{CapVoice}},
		{"all granted", []string{CapVoice, CapCanvas}, allCapabilities, []string{CapVoice, CapCanvas}},
		{"none allowed", []string{CapVoice}, []string{CapCanvas}, nil},
		{"unknown dropped", []string{"telepathy", CapScreen}, []string{CapScreen}, []string{CapScreen}},
		{"deterministic order", []string{CapScreen, CapVoice}, allCapabilities, []string{CapVoice, CapScreen}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := negotiateCapabilities(tt.offered, tt.allowed)
			if strings.Join(got, ",") != strings.Join(tt.expected, ",") {
				t.Fatalf("got %v want %v", got, tt.expected)
			}
		})
	}
}

func TestHandleValidPairAndNegotiate(t *testing.T) {
	s := newStore(t)
	srv := NewServer(s, nil)
	code, _ := s.IssueCode("acme", "phone-1", []string{CapVoice, CapCanvas})

	conn := &stubConn{in: []Frame{
		{Type: MsgPair, Code: code, Node: "phone-1"},
		{Type: MsgHello, Capabilities: []string{CapVoice, CapCamera}},
		{Type: MsgVoice, Transcript: "hello"},
		{Type: MsgCanvas, Canvas: map[string]any{"op": "draw"}},
	}}
	sess := srv.handle(conn)
	if sess == nil {
		t.Fatal("expected a session after valid pairing")
	}
	if sess.Tenant != "acme" {
		t.Fatalf("tenant not bound from pairing: %q", sess.Tenant)
	}
	welcome, ok := conn.firstOfType(MsgWelcome)
	if !ok {
		t.Fatal("expected welcome frame")
	}
	// camera was offered but not granted by the pairing => negotiated only voice.
	if strings.Join(welcome.Capabilities, ",") != CapVoice {
		t.Fatalf("negotiated caps = %v, want [voice]", welcome.Capabilities)
	}
	if welcome.Tenant != "acme" {
		t.Fatalf("welcome must carry bound tenant, got %q", welcome.Tenant)
	}
	// voice frame acked; canvas frame rejected (not negotiated).
	var voiceAcked, canvasRejected bool
	for _, f := range conn.out {
		if f.Type == MsgAck && f.Ref == MsgVoice {
			voiceAcked = true
		}
		if f.Type == MsgError && f.ErrorCode == ErrNotNegotiated {
			canvasRejected = true
		}
	}
	if !voiceAcked {
		t.Fatal("voice frame should be acked")
	}
	if !canvasRejected {
		t.Fatal("canvas frame should be rejected (capability not negotiated)")
	}
}

func TestHandleRejectsMissingPairing(t *testing.T) {
	srv := NewServer(newStore(t), nil)
	// First frame is not a pair frame -> protocol error, no session.
	conn := &stubConn{in: []Frame{{Type: MsgHello, Capabilities: []string{CapVoice}}}}
	if sess := srv.handle(conn); sess != nil {
		t.Fatal("non-pair first frame must not establish a session")
	}
	if conn.last().Type != MsgError {
		t.Fatalf("expected error frame, got %q", conn.last().Type)
	}
	if !conn.closed {
		t.Fatal("connection must be closed on failed pairing")
	}
}

func TestHandleRejectsInvalidCode(t *testing.T) {
	srv := NewServer(newStore(t), nil)
	conn := &stubConn{in: []Frame{{Type: MsgPair, Code: "BADCODE", Node: "x"}}}
	if sess := srv.handle(conn); sess != nil {
		t.Fatal("invalid code must not establish a session")
	}
	e := conn.last()
	if e.Type != MsgError || e.ErrorCode != ErrBadPairing {
		t.Fatalf("expected BAD_PAIRING error, got %+v", e)
	}
}

func TestHandleRejectsEmptyCode(t *testing.T) {
	srv := NewServer(newStore(t), nil)
	conn := &stubConn{in: []Frame{{Type: MsgPair, Code: "", Node: "x"}}}
	if sess := srv.handle(conn); sess != nil {
		t.Fatal("empty code must fail closed")
	}
	if conn.last().Type != MsgError {
		t.Fatalf("expected error frame, got %q", conn.last().Type)
	}
}

// Tenant isolation: a code issued for tenant A binds the session to A only; the
// client cannot assert a different tenant on its frames.
func TestTenantIsolation(t *testing.T) {
	s := newStore(t)
	srv := NewServer(s, nil)
	codeA, _ := s.IssueCode("tenantA", "node-1", allCapabilities)

	conn := &stubConn{in: []Frame{
		// Client lies about tenant in the frame; server must ignore it.
		{Type: MsgPair, Code: codeA, Node: "node-1", Tenant: "tenantB"},
		{Type: MsgHello, Capabilities: []string{CapVoice}},
		{Type: MsgVoice, Transcript: "x", Tenant: "tenantB", Session: "forged"},
	}}
	sess := srv.handle(conn)
	if sess == nil || sess.Tenant != "tenantA" {
		t.Fatalf("session must be bound to tenantA, got %+v", sess)
	}
	// The voice ack must echo the server-bound tenant, never the forged one.
	for _, f := range conn.out {
		if f.Type == MsgAck && f.Ref == MsgVoice {
			if f.Tenant != "tenantA" {
				t.Fatalf("ack leaked/forged tenant: %q", f.Tenant)
			}
		}
	}
	// A code from tenantA can never be redeemed as tenantB — redemption is keyed
	// only by code, and the bound tenant comes from the pending record.
	if p, _ := s.AllowedCaps("tenantB", "node-1"); p != nil {
		t.Fatal("node must not be paired under tenantB")
	}
}

func TestPairFrameWithInlineCapabilities(t *testing.T) {
	s := newStore(t)
	srv := NewServer(s, nil)
	code, _ := s.IssueCode("acme", "n", []string{CapVoice})
	// Capabilities on the pair frame skip the separate hello.
	conn := &stubConn{in: []Frame{
		{Type: MsgPair, Code: code, Node: "n", Capabilities: []string{CapVoice}},
	}}
	sess := srv.handle(conn)
	if sess == nil || strings.Join(sess.Capabilities, ",") != CapVoice {
		t.Fatalf("inline-capability pairing failed: %+v", sess)
	}
}

// End-to-end over a real WebSocket via httptest, exercising the gorilla adapter.
func TestWebSocketRoundTrip(t *testing.T) {
	s := newStore(t)
	srv := NewServer(s, nil)
	code, _ := s.IssueCode("acme", "browser", []string{CapVoice, CapCanvas})

	ts := httptest.NewServer(http.HandlerFunc(srv.ServeHTTP))
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	if err := c.WriteJSON(Frame{Type: MsgPair, Code: code, Node: "browser"}); err != nil {
		t.Fatalf("write pair: %v", err)
	}
	if err := c.WriteJSON(Frame{Type: MsgHello, Capabilities: []string{CapVoice, CapCanvas}}); err != nil {
		t.Fatalf("write hello: %v", err)
	}
	var welcome Frame
	if err := c.ReadJSON(&welcome); err != nil {
		t.Fatalf("read welcome: %v", err)
	}
	if welcome.Type != MsgWelcome || welcome.Session == "" || welcome.Tenant != "acme" {
		t.Fatalf("bad welcome: %+v", welcome)
	}
	if len(welcome.Capabilities) != 2 {
		t.Fatalf("expected 2 negotiated caps, got %v", welcome.Capabilities)
	}

	// Sample voice + canvas messages should be acked.
	c.WriteJSON(Frame{Type: MsgVoice, Transcript: "hi"})
	var ack Frame
	if err := c.ReadJSON(&ack); err != nil {
		t.Fatalf("read voice ack: %v", err)
	}
	if ack.Type != MsgAck || ack.Ref != MsgVoice || ack.Tenant != "acme" {
		t.Fatalf("bad voice ack: %+v", ack)
	}

	c.WriteJSON(Frame{Type: MsgCanvas, Canvas: map[string]any{"stroke": []int{1, 2}}})
	if err := c.ReadJSON(&ack); err != nil {
		t.Fatalf("read canvas ack: %v", err)
	}
	if ack.Type != MsgAck || ack.Ref != MsgCanvas {
		t.Fatalf("bad canvas ack: %+v", ack)
	}
}

// A WS connection that never presents a valid code is rejected (auth enforced).
func TestWebSocketRejectsUnpaired(t *testing.T) {
	srv := NewServer(newStore(t), nil)
	ts := httptest.NewServer(http.HandlerFunc(srv.ServeHTTP))
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	c.WriteJSON(Frame{Type: MsgPair, Code: "NOPE", Node: "x"})
	var resp Frame
	if err := c.ReadJSON(&resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != MsgError || resp.ErrorCode != ErrBadPairing {
		t.Fatalf("expected BAD_PAIRING, got %+v", resp)
	}
	// Server must close the connection after a failed pairing.
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("connection should be closed after failed pairing")
	}
}
