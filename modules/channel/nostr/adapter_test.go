package nostr

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	secp "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/gorilla/websocket"

	"github.com/cyntr-dev/cyntr/modules/channel"
)

// testPrivHex is a deterministic 32-byte secp256k1 private key for signing tests.
const testPrivHex = "0000000000000000000000000000000000000000000000000000000000000003"

// fakeRelay is an httptest server that upgrades to a websocket and records
// frames sent by the adapter. It can also push EVENT frames to the client.
type fakeRelay struct {
	srv      *httptest.Server
	upgrader websocket.Upgrader

	mu       sync.Mutex
	received [][]byte
	conns    []*websocket.Conn
	connCh   chan *websocket.Conn
}

func newFakeRelay(t *testing.T) *fakeRelay {
	t.Helper()
	fr := &fakeRelay{connCh: make(chan *websocket.Conn, 8)}
	fr.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := fr.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		fr.mu.Lock()
		fr.conns = append(fr.conns, conn)
		fr.mu.Unlock()
		select {
		case fr.connCh <- conn:
		default:
		}
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			fr.mu.Lock()
			cp := make([]byte, len(data))
			copy(cp, data)
			fr.received = append(fr.received, cp)
			fr.mu.Unlock()
		}
	}))
	t.Cleanup(fr.srv.Close)
	return fr
}

func (fr *fakeRelay) wsURL() string {
	return "ws" + strings.TrimPrefix(fr.srv.URL, "http")
}

func (fr *fakeRelay) waitConn(t *testing.T) *websocket.Conn {
	t.Helper()
	select {
	case c := <-fr.connCh:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for relay connection")
		return nil
	}
}

func (fr *fakeRelay) frames() [][]byte {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	out := make([][]byte, len(fr.received))
	copy(out, fr.received)
	return out
}

// newTestAdapter wires an adapter to dial the fake relay regardless of the
// configured URL, so we control the connection.
func newTestAdapter(t *testing.T, fr *fakeRelay, privHex, tenant, agent string) *Adapter {
	t.Helper()
	a, err := New([]string{fr.wsURL()}, privHex, tenant, agent)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.SetNowFunc(func() int64 { return 1700000000 })
	return a
}

// TestInboundEventToInboundMessage verifies a relay EVENT frame is translated
// into a channel.InboundMessage carrying the configured tenant + agent.
func TestInboundEventToInboundMessage(t *testing.T) {
	fr := newFakeRelay(t)
	a := newTestAdapter(t, fr, "", "acme", "support")

	got := make(chan channel.InboundMessage, 1)
	handler := func(msg channel.InboundMessage) (string, error) {
		got <- msg
		return "", nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx, handler); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(context.Background())

	conn := fr.waitConn(t)

	// Push an EVENT frame as a relay would.
	ev := Event{
		ID:        "abc",
		PubKey:    "deadbeef",
		CreatedAt: 1700000001,
		Kind:      kindTextNote,
		Tags:      [][]string{},
		Content:   "hello from nostr",
		Sig:       "00",
	}
	frame, _ := json.Marshal([]any{"EVENT", "cyntr-support", ev})
	if err := conn.WriteMessage(websocket.TextMessage, frame); err != nil {
		t.Fatalf("relay write: %v", err)
	}

	select {
	case msg := <-got:
		if msg.Tenant != "acme" {
			t.Errorf("tenant = %q, want acme", msg.Tenant)
		}
		if msg.Agent != "support" {
			t.Errorf("agent = %q, want support", msg.Agent)
		}
		if msg.Channel != "nostr" {
			t.Errorf("channel = %q, want nostr", msg.Channel)
		}
		if msg.Text != "hello from nostr" {
			t.Errorf("text = %q", msg.Text)
		}
		if msg.UserID != "deadbeef" {
			t.Errorf("userID = %q, want deadbeef", msg.UserID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called for inbound EVENT")
	}
}

// TestInboundIgnoresNonEvents ensures EOSE/NOTICE/OK frames and wrong kinds
// do not trigger the handler.
func TestInboundIgnoresNonEvents(t *testing.T) {
	fr := newFakeRelay(t)
	a := newTestAdapter(t, fr, "", "t", "agt")

	called := make(chan struct{}, 1)
	handler := func(msg channel.InboundMessage) (string, error) {
		called <- struct{}{}
		return "", nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx, handler); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(context.Background())
	conn := fr.waitConn(t)

	frames := [][]any{
		{"EOSE", "cyntr-agt"},
		{"NOTICE", "rate limited"},
		{"OK", "eventid", true, ""},
		{"EVENT", "cyntr-agt", Event{Kind: 9999, Content: "ignored kind", PubKey: "x"}},
	}
	for _, f := range frames {
		b, _ := json.Marshal(f)
		conn.WriteMessage(websocket.TextMessage, b)
	}

	select {
	case <-called:
		t.Fatal("handler should not be called for non-event/irrelevant frames")
	case <-time.After(300 * time.Millisecond):
		// success: nothing dispatched
	}
}

// TestSubscribeSendsREQ verifies a REQ frame is emitted on connect with the
// expected kinds filter.
func TestSubscribeSendsREQ(t *testing.T) {
	fr := newFakeRelay(t)
	a := newTestAdapter(t, fr, "", "t", "agt")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx, func(channel.InboundMessage) (string, error) { return "", nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(context.Background())
	fr.waitConn(t)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("no REQ frame received")
		default:
		}
		for _, f := range fr.frames() {
			var arr []json.RawMessage
			if json.Unmarshal(f, &arr) != nil || len(arr) < 3 {
				continue
			}
			var typ string
			json.Unmarshal(arr[0], &typ)
			if typ != "REQ" {
				continue
			}
			var filter map[string]any
			if json.Unmarshal(arr[2], &filter) != nil {
				continue
			}
			kinds, ok := filter["kinds"].([]any)
			if !ok || len(kinds) != 2 {
				t.Fatalf("REQ kinds malformed: %v", filter["kinds"])
			}
			return // success
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSendBuildsValidEvent verifies Send publishes an EVENT whose id and
// signature are valid (BIP-340), proving the signing path works end to end.
func TestSendBuildsValidEvent(t *testing.T) {
	fr := newFakeRelay(t)
	a := newTestAdapter(t, fr, testPrivHex, "acme", "support")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := a.Start(ctx, func(channel.InboundMessage) (string, error) { return "", nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer a.Stop(context.Background())
	fr.waitConn(t)

	// Give the read goroutine a moment to register the conn.
	if err := waitFor(func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return len(a.conns) == 1
	}, 2*time.Second); err != nil {
		t.Fatal("relay conn not registered")
	}

	if err := a.Send(ctx, channel.OutboundMessage{Text: "signed hello"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if err := waitFor(func() bool {
		for _, f := range fr.frames() {
			var arr []json.RawMessage
			if json.Unmarshal(f, &arr) == nil && len(arr) == 2 {
				var typ string
				json.Unmarshal(arr[0], &typ)
				if typ == "EVENT" {
					return true
				}
			}
		}
		return false
	}, 2*time.Second); err != nil {
		t.Fatal("no EVENT frame published")
	}

	// Decode the published event and validate id + signature.
	var ev Event
	for _, f := range fr.frames() {
		var arr []json.RawMessage
		if json.Unmarshal(f, &arr) != nil || len(arr) != 2 {
			continue
		}
		var typ string
		json.Unmarshal(arr[0], &typ)
		if typ != "EVENT" {
			continue
		}
		if err := json.Unmarshal(arr[1], &ev); err != nil {
			t.Fatalf("decode event: %v", err)
		}
	}
	if ev.Content != "signed hello" {
		t.Fatalf("content = %q", ev.Content)
	}
	if ev.Kind != kindTextNote {
		t.Fatalf("kind = %d", ev.Kind)
	}
	if ev.PubKey != a.PubKey() {
		t.Fatalf("pubkey = %q, want %q", ev.PubKey, a.PubKey())
	}

	// Recompute id and verify it matches.
	idBytes, err := eventID(ev)
	if err != nil {
		t.Fatalf("eventID: %v", err)
	}
	if hex.EncodeToString(idBytes) != ev.ID {
		t.Fatalf("event id mismatch: got %s want %s", ev.ID, hex.EncodeToString(idBytes))
	}

	// Verify the BIP-340 signature against the id.
	sig, err := hex.DecodeString(ev.Sig)
	if err != nil || len(sig) != 64 {
		t.Fatalf("bad sig hex (len %d): %v", len(sig), err)
	}
	if !verifyEventSig(ev.PubKey, idBytes, sig) {
		t.Fatal("BIP-340 signature did not verify")
	}
}

// TestUnsignedPath verifies that without a key, BuildEvent yields a correct id
// but empty sig, and Send refuses to publish.
func TestUnsignedPath(t *testing.T) {
	a, err := New([]string{"ws://relay.example"}, "", "t", "agt")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.SetNowFunc(func() int64 { return 1700000000 })

	ev, err := a.BuildEvent(kindTextNote, "no key here", nil)
	if err != nil {
		t.Fatalf("BuildEvent: %v", err)
	}
	if ev.Sig != "" {
		t.Errorf("expected empty sig, got %q", ev.Sig)
	}
	idBytes, _ := eventID(ev)
	if hex.EncodeToString(idBytes) != ev.ID {
		t.Errorf("id mismatch on unsigned path")
	}
	if ev.PubKey != "" {
		t.Errorf("expected empty pubkey, got %q", ev.PubKey)
	}

	if err := a.Send(context.Background(), channel.OutboundMessage{Text: "x"}); err == nil {
		t.Error("Send should fail without a signing key")
	}
}

// TestSignVerifyRoundTrip is a focused unit test of the BIP-340 implementation.
func TestSignVerifyRoundTrip(t *testing.T) {
	cases := []string{
		"0000000000000000000000000000000000000000000000000000000000000001",
		"0000000000000000000000000000000000000000000000000000000000000003",
		"b7e151628aed2a6abf7158809cf4f3c762e7160f38b4da56a784d9045190cfef",
	}
	msg := make([]byte, 32)
	for i := range msg {
		msg[i] = byte(i + 1)
	}
	for _, hexKey := range cases {
		kb, _ := hex.DecodeString(hexKey)
		priv := secp.PrivKeyFromBytes(kb)
		pubX := hex.EncodeToString(priv.PubKey().SerializeCompressed()[1:])

		sig, err := signEventHash(priv, msg)
		if err != nil {
			t.Fatalf("sign %s: %v", hexKey, err)
		}
		if len(sig) != 64 {
			t.Fatalf("sig len = %d", len(sig))
		}
		if !verifyEventSig(pubX, msg, sig) {
			t.Errorf("verify failed for key %s", hexKey)
		}
		// Tamper -> must fail.
		bad := make([]byte, 32)
		copy(bad, msg)
		bad[0] ^= 0xff
		if verifyEventSig(pubX, bad, sig) {
			t.Errorf("verify wrongly succeeded for tampered msg, key %s", hexKey)
		}
	}
}

// TestCleanStartStop verifies Stop tears down cleanly and is idempotent.
func TestCleanStartStop(t *testing.T) {
	fr := newFakeRelay(t)
	a := newTestAdapter(t, fr, "", "t", "agt")

	ctx := context.Background()
	if err := a.Start(ctx, func(channel.InboundMessage) (string, error) { return "", nil }); err != nil {
		t.Fatalf("Start: %v", err)
	}
	fr.waitConn(t)

	if err := a.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Idempotent.
	if err := a.Stop(ctx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	// Send after stop must fail.
	if err := a.Send(ctx, channel.OutboundMessage{Text: "x"}); err == nil {
		t.Error("Send after Stop should fail")
	}
	// Start after stop must fail (closed).
	if err := a.Start(ctx, func(channel.InboundMessage) (string, error) { return "", nil }); err == nil {
		t.Error("Start after Stop should fail")
	}
}

// TestNewValidations covers config validation.
func TestNewValidations(t *testing.T) {
	if _, err := New([]string{"ws://r"}, "nothex!!", "t", "a"); err == nil {
		t.Error("expected error for invalid private key hex")
	}
	if _, err := New([]string{"ws://r"}, "00", "t", "a"); err == nil {
		t.Error("expected error for short private key")
	}
	a, err := New([]string{" ws://r ", ""}, "", "", "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.tenant != "default" || a.agent != "assistant" {
		t.Errorf("defaults not applied: tenant=%q agent=%q", a.tenant, a.agent)
	}
	if len(a.relays) != 1 || a.relays[0] != "ws://r" {
		t.Errorf("relay normalization failed: %v", a.relays)
	}
}

func waitFor(cond func() bool, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	if cond() {
		return nil
	}
	return context.DeadlineExceeded
}
