// Package nostr implements a Channel Manager adapter for the Nostr protocol
// (NIP-01) using gorilla/websocket. It maintains persistent websocket
// connections to one or more relays, subscribes (REQ) for inbound events
// (kind 1 text notes and kind 4 encrypted DMs) and translates them into
// channel.InboundMessage carrying the configured tenant + agent. Outbound
// messages are published as signed kind-1 EVENT frames.
//
// Signing: Nostr event IDs are sha256 over the canonical serialization
// (NIP-01) — computed here exactly with the standard library. Event
// signatures are BIP-340 Schnorr over the secp256k1 curve. We implement
// BIP-340 directly on top of the secp256k1 field/scalar/group primitives that
// are ALREADY present in go.mod
// (github.com/decred/dcrd/dcrec/secp256k1/v4) plus crypto/sha256 and
// crypto/rand. We deliberately do NOT use that module's own schnorr
// subpackage: it implements the "EC-Schnorr-DCRv0" scheme, which is NOT
// BIP-340 and would produce signatures that real Nostr relays reject. See
// signEventHash for the full BIP-340 implementation.
//
// The adapter is enabled only when configured (see cmd wiring): an empty
// relay list means the adapter is never constructed/registered. When no
// private key is configured the adapter still runs inbound (REQ) and builds
// events for outbound, but leaves the signature empty and refuses to Send —
// see the unsigned path covered by tests.
package nostr

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	secp "github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/gorilla/websocket"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

var logger = log.Default().WithModule("channel_nostr")

// Compile-time assertion that Adapter satisfies the ChannelAdapter contract.
var _ channel.ChannelAdapter = (*Adapter)(nil)

// Nostr event kinds we care about.
const (
	kindTextNote = 1 // NIP-01 short text note
	kindDM       = 4 // NIP-04 encrypted direct message (treated as opaque inbound)
)

// dialFunc abstracts the websocket dial so tests can point the adapter at an
// httptest relay. Defaults to gorilla's DefaultDialer with a connect timeout.
type dialFunc func(ctx context.Context, urlStr string) (*websocket.Conn, *http.Response, error)

// relayConn wraps a websocket connection with a write mutex. gorilla/websocket
// permits at most one concurrent writer per connection, so every outbound
// frame (REQ subscriptions, replies, published EVENTs) is serialized here.
type relayConn struct {
	conn  *websocket.Conn
	wmu   sync.Mutex
	relay string
}

func (rc *relayConn) write(b []byte) error {
	rc.wmu.Lock()
	defer rc.wmu.Unlock()
	return rc.conn.WriteMessage(websocket.TextMessage, b)
}

func (rc *relayConn) close() { rc.conn.Close() }

// Event is the minimal NIP-01 event structure.
//
// The canonical id is the lowercase hex of sha256 over the JSON array
//
//	[0, pubkey, created_at, kind, tags, content]
//
// and sig is the BIP-340 Schnorr signature of that 32-byte id.
type Event struct {
	ID        string     `json:"id"`
	PubKey    string     `json:"pubkey"`
	CreatedAt int64      `json:"created_at"`
	Kind      int        `json:"kind"`
	Tags      [][]string `json:"tags"`
	Content   string     `json:"content"`
	Sig       string     `json:"sig"`
}

// Adapter is a Nostr channel adapter implementing channel.ChannelAdapter.
type Adapter struct {
	relays []string // websocket relay URLs (wss://... or ws://...)
	tenant string   // tenant attached to every inbound message
	agent  string   // target agent attached to every inbound message

	priv   *secp.PrivateKey // signing key; nil => unsigned (Send refused)
	pubHex string           // 32-byte x-only pubkey hex (NIP-01 "pubkey")

	dial    dialFunc
	handler channel.InboundHandler

	// now/sub are overridable in tests.
	now func() int64

	mu     sync.Mutex
	conns  map[string]*relayConn // active relay connections by URL
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed bool
}

// New constructs a Nostr adapter for the given relays. privKeyHex is an
// optional 32-byte hex-encoded secp256k1 private key used to sign outbound
// notes; when empty the adapter still receives inbound events and builds
// (unsigned) events, but Send returns an error rather than publishing an
// unsigned note. tenant/agent are attached to every inbound message.
func New(relays []string, privKeyHex, tenant, agent string) (*Adapter, error) {
	norm := make([]string, 0, len(relays))
	for _, r := range relays {
		r = strings.TrimSpace(r)
		if r != "" {
			norm = append(norm, r)
		}
	}
	if tenant == "" {
		tenant = "default"
	}
	if agent == "" {
		agent = "assistant"
	}
	a := &Adapter{
		relays: norm,
		tenant: tenant,
		agent:  agent,
		conns:  make(map[string]*relayConn),
		now:    func() int64 { return time.Now().Unix() },
		dial: func(ctx context.Context, urlStr string) (*websocket.Conn, *http.Response, error) {
			d := websocket.Dialer{HandshakeTimeout: 30 * time.Second}
			return d.DialContext(ctx, urlStr, nil)
		},
	}
	if privKeyHex = strings.TrimSpace(privKeyHex); privKeyHex != "" {
		kb, err := hex.DecodeString(privKeyHex)
		if err != nil || len(kb) != 32 {
			return nil, fmt.Errorf("nostr: invalid private key hex (want 32-byte hex): %v", err)
		}
		a.priv = secp.PrivKeyFromBytes(kb)
		// NIP-01 pubkey is the 32-byte x-only coordinate.
		xb := a.priv.PubKey().SerializeCompressed()[1:] // drop the parity byte
		a.pubHex = hex.EncodeToString(xb)
	}
	return a, nil
}

// SetDialFunc overrides the websocket dialer (used by tests).
func (a *Adapter) SetDialFunc(f dialFunc) { a.dial = f }

// SetNowFunc overrides the clock (used by tests).
func (a *Adapter) SetNowFunc(f func() int64) { a.now = f }

func (a *Adapter) Name() string { return "nostr" }

// PubKey returns the configured x-only public key hex ("" when unsigned).
func (a *Adapter) PubKey() string { return a.pubHex }

// Start dials every configured relay and begins reading inbound events. Each
// relay runs its own goroutine with reconnect-with-backoff. It honors ctx:
// cancelling ctx (or calling Stop) tears down all connections and exits the
// read loops.
func (a *Adapter) Start(ctx context.Context, handler channel.InboundHandler) error {
	a.handler = handler

	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return fmt.Errorf("nostr: adapter already stopped")
	}
	runCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.mu.Unlock()

	if len(a.relays) == 0 {
		return fmt.Errorf("nostr: no relays configured")
	}

	for _, relay := range a.relays {
		a.wg.Add(1)
		go a.relayLoop(runCtx, relay)
	}
	return nil
}

// Stop cancels all relay loops and closes connections. Safe to call multiple
// times. Blocks until every read goroutine has exited, bounded by ctx.
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	a.closed = true
	if a.cancel != nil {
		a.cancel()
	}
	for _, c := range a.conns {
		c.close()
	}
	a.conns = map[string]*relayConn{}
	a.mu.Unlock()

	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// relayLoop maintains one relay connection, reconnecting with exponential
// backoff until ctx is cancelled.
func (a *Adapter) relayLoop(ctx context.Context, relay string) {
	defer a.wg.Done()

	const (
		baseBackoff = 500 * time.Millisecond
		maxBackoff  = 30 * time.Second
	)
	backoff := baseBackoff

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, _, err := a.dial(ctx, relay)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		rc := &relayConn{conn: conn, relay: relay}

		a.mu.Lock()
		stopped := a.closed
		if !stopped {
			a.conns[relay] = rc
		}
		a.mu.Unlock()
		if stopped {
			rc.close()
			return
		}

		backoff = baseBackoff // healthy connection resets backoff
		a.subscribe(rc)
		a.readConn(ctx, rc)

		a.mu.Lock()
		if a.conns[relay] == rc {
			delete(a.conns, relay)
		}
		a.mu.Unlock()
		rc.close()

		// Loop to reconnect (unless ctx is done, handled at top).
	}
}

// subscribe sends a REQ frame requesting recent kind-1 and kind-4 events. We
// scope the filter to "since now" so we don't replay history on reconnect.
func (a *Adapter) subscribe(rc *relayConn) {
	filter := map[string]any{
		"kinds": []int{kindTextNote, kindDM},
		"since": a.now(),
	}
	req := []any{"REQ", a.subID(), filter}
	b, err := json.Marshal(req)
	if err != nil {
		logger.Error("nostr REQ marshal failed", map[string]any{"error": err.Error()})
		return
	}
	if err := rc.write(b); err != nil {
		logger.Warn("nostr REQ write failed", map[string]any{"error": err.Error()})
	}
}

// subID returns a stable subscription id for this adapter instance.
func (a *Adapter) subID() string { return "cyntr-" + a.agent }

// readConn consumes relay frames until ctx is cancelled or the socket closes.
func (a *Adapter) readConn(ctx context.Context, rc *relayConn) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_, data, err := rc.conn.ReadMessage()
		if err != nil {
			select {
			case <-ctx.Done():
				// expected on Stop
			default:
				logger.Warn("nostr read loop ended", map[string]any{"relay": rc.relay, "error": err.Error()})
			}
			return
		}
		a.handleFrame(data)
	}
}

// handleFrame parses one relay->client frame. We only act on EVENT frames:
//
//	["EVENT", <subscription_id>, <event JSON object>]
func (a *Adapter) handleFrame(data []byte) {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || len(raw) == 0 {
		return
	}
	var typ string
	if err := json.Unmarshal(raw[0], &typ); err != nil {
		return
	}
	if typ != "EVENT" || len(raw) < 3 {
		return // ignore EOSE / NOTICE / OK / etc.
	}

	var ev Event
	if err := json.Unmarshal(raw[2], &ev); err != nil {
		logger.Warn("nostr event decode failed", map[string]any{"error": err.Error()})
		return
	}
	if ev.Kind != kindTextNote && ev.Kind != kindDM {
		return
	}
	// Skip our own published notes echoed back by the relay.
	if a.pubHex != "" && ev.PubKey == a.pubHex {
		return
	}

	go a.dispatch(ev)
}

// dispatch converts an inbound event into a channel.InboundMessage with the
// configured tenant + agent and, if the handler returns a reply, publishes it.
func (a *Adapter) dispatch(ev Event) {
	response, err := a.handler(channel.InboundMessage{
		Channel:   "nostr",
		ChannelID: ev.PubKey, // reply target is the author's pubkey
		UserID:    ev.PubKey,
		Text:      ev.Content,
		Tenant:    a.tenant,
		Agent:     a.agent,
	})
	if err != nil {
		logger.Error("message handler failed", map[string]any{"error": err.Error()})
		return
	}
	if strings.TrimSpace(response) == "" {
		return
	}
	if err := a.Send(context.Background(), channel.OutboundMessage{
		Channel: "nostr",
		Text:    response,
	}); err != nil {
		logger.Warn("nostr reply failed", map[string]any{"error": err.Error()})
	}
}

// Send builds, signs, and publishes a kind-1 text note to every connected
// relay. It requires a configured private key; without one it returns an
// error rather than broadcasting an unsigned (relay-rejected) event.
func (a *Adapter) Send(ctx context.Context, msg channel.OutboundMessage) error {
	if strings.TrimSpace(msg.Text) == "" {
		return fmt.Errorf("nostr: empty message")
	}
	if a.priv == nil {
		return fmt.Errorf("nostr: no signing key configured; cannot publish")
	}

	ev, err := a.BuildEvent(kindTextNote, msg.Text, nil)
	if err != nil {
		return fmt.Errorf("nostr build event: %w", err)
	}

	frame, err := json.Marshal([]any{"EVENT", ev})
	if err != nil {
		return fmt.Errorf("nostr marshal EVENT: %w", err)
	}

	a.mu.Lock()
	conns := make([]*relayConn, 0, len(a.conns))
	for _, c := range a.conns {
		conns = append(conns, c)
	}
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return fmt.Errorf("nostr: adapter stopped")
	}
	if len(conns) == 0 {
		return fmt.Errorf("nostr: no connected relays")
	}

	var lastErr error
	sent := 0
	for _, c := range conns {
		if err := c.write(frame); err != nil {
			lastErr = err
			continue
		}
		sent++
	}
	if sent == 0 {
		return fmt.Errorf("nostr publish failed on all relays: %w", lastErr)
	}
	return nil
}

// BuildEvent constructs a NIP-01 event, computes its id (sha256 of the
// canonical serialization), and signs it (BIP-340) when a private key is
// present. When no key is present the returned event has the correct id but an
// empty Sig — this is the unit-tested unsigned path.
func (a *Adapter) BuildEvent(kind int, content string, tags [][]string) (Event, error) {
	if tags == nil {
		tags = [][]string{}
	}
	ev := Event{
		PubKey:    a.pubHex,
		CreatedAt: a.now(),
		Kind:      kind,
		Tags:      tags,
		Content:   content,
	}
	id, err := eventID(ev)
	if err != nil {
		return Event{}, err
	}
	ev.ID = hex.EncodeToString(id)

	if a.priv != nil {
		sig, err := signEventHash(a.priv, id)
		if err != nil {
			return Event{}, fmt.Errorf("nostr sign: %w", err)
		}
		ev.Sig = hex.EncodeToString(sig)
	}
	return ev, nil
}

// eventID computes the 32-byte NIP-01 event id: sha256 over the compact JSON
// array [0, pubkey, created_at, kind, tags, content]. The serialization MUST
// be exactly this shape with no extra whitespace; we marshal each element and
// assemble manually to guarantee field order and compactness.
func eventID(ev Event) ([]byte, error) {
	tags := ev.Tags
	if tags == nil {
		tags = [][]string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return nil, fmt.Errorf("marshal tags: %w", err)
	}
	contentJSON, err := json.Marshal(ev.Content)
	if err != nil {
		return nil, fmt.Errorf("marshal content: %w", err)
	}
	var b strings.Builder
	b.WriteString("[0,")
	b.WriteString(strconv.Quote(ev.PubKey))
	b.WriteByte(',')
	b.WriteString(strconv.FormatInt(ev.CreatedAt, 10))
	b.WriteByte(',')
	b.WriteString(strconv.Itoa(ev.Kind))
	b.WriteByte(',')
	b.Write(tagsJSON)
	b.WriteByte(',')
	b.Write(contentJSON)
	b.WriteByte(']')

	sum := sha256.Sum256([]byte(b.String()))
	return sum[:], nil
}

// --- BIP-340 Schnorr signing over secp256k1 -------------------------------
//
// Implemented directly against the secp256k1 primitives already in go.mod.
// This follows BIP-340 (the scheme Nostr/NIP-01 mandates). It is intentionally
// independent of the decred "schnorr" subpackage, whose EC-Schnorr-DCRv0
// scheme is incompatible with Nostr.

// taggedHash computes sha256(sha256(tag) || sha256(tag) || msg...) per BIP-340.
func taggedHash(tag string, msgs ...[]byte) [32]byte {
	th := sha256.Sum256([]byte(tag))
	h := sha256.New()
	h.Write(th[:])
	h.Write(th[:])
	for _, m := range msgs {
		h.Write(m)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// signEventHash produces a 64-byte BIP-340 Schnorr signature over the 32-byte
// message hash using the given secp256k1 private key.
func signEventHash(priv *secp.PrivateKey, msg []byte) ([]byte, error) {
	if len(msg) != 32 {
		return nil, fmt.Errorf("message must be 32 bytes, got %d", len(msg))
	}

	// d' = secret key as scalar.
	var d secp.ModNScalar
	d.Set(&priv.Key)
	if d.IsZero() {
		return nil, fmt.Errorf("invalid (zero) private key")
	}

	// P = d'*G. If P has odd Y, negate d so the effective key has even Y
	// (BIP-340 uses x-only public keys with implicit even Y).
	var P secp.JacobianPoint
	secp.ScalarBaseMultNonConst(&d, &P)
	P.ToAffine()
	if P.Y.IsOdd() {
		d.Negate()
	}
	pBytes := P.X.Bytes() // x-only pubkey (32 bytes)

	dBytes := d.Bytes()

	// Nonce generation. BIP-340 derives a deterministic nonce; we use fresh
	// randomness for the auxiliary data (aux_rand) which BIP-340 explicitly
	// permits. t = d XOR tagged_hash("BIP0340/aux", aux_rand).
	var aux [32]byte
	if _, err := rand.Read(aux[:]); err != nil {
		return nil, fmt.Errorf("read aux rand: %w", err)
	}
	auxHash := taggedHash("BIP0340/aux", aux[:])
	var t [32]byte
	for i := 0; i < 32; i++ {
		t[i] = dBytes[i] ^ auxHash[i]
	}

	// rand = tagged_hash("BIP0340/nonce", t || P_x || msg).
	nonceHash := taggedHash("BIP0340/nonce", t[:], pBytes[:], msg)
	var k secp.ModNScalar
	k.SetBytes(&nonceHash)
	if k.IsZero() {
		return nil, fmt.Errorf("derived zero nonce")
	}

	// R = k'*G. If R has odd Y, negate k.
	var R secp.JacobianPoint
	secp.ScalarBaseMultNonConst(&k, &R)
	R.ToAffine()
	if R.Y.IsOdd() {
		k.Negate()
	}
	rBytes := R.X.Bytes() // R.x (32 bytes)

	// e = tagged_hash("BIP0340/challenge", R.x || P.x || msg) mod n.
	eHash := taggedHash("BIP0340/challenge", rBytes[:], pBytes[:], msg)
	var e secp.ModNScalar
	e.SetBytes(&eHash)

	// s = k + e*d mod n.
	var s secp.ModNScalar
	s.Mul2(&e, &d) // e*d
	s.Add(&k)      // + k
	sBytes := s.Bytes()

	// Signature = R.x (32) || s (32).
	sig := make([]byte, 64)
	copy(sig[0:32], rBytes[:])
	copy(sig[32:64], sBytes[:])

	// Zero sensitive scalars.
	d.Zero()
	k.Zero()
	s.Zero()

	return sig, nil
}

// verifyEventSig verifies a BIP-340 signature; used by tests to prove the
// signing path produces Nostr-valid signatures.
func verifyEventSig(pubXHex string, msg, sig []byte) bool {
	if len(sig) != 64 || len(msg) != 32 {
		return false
	}
	pxBytes, err := hex.DecodeString(pubXHex)
	if err != nil || len(pxBytes) != 32 {
		return false
	}

	// Lift x to a point with even Y.
	var px secp.FieldVal
	var pxArr [32]byte
	copy(pxArr[:], pxBytes)
	if overflow := px.SetBytes(&pxArr); overflow != 0 {
		return false
	}
	var P secp.JacobianPoint
	if !secp.DecompressY(&px, false, &P.Y) {
		return false
	}
	P.X.Set(&px)
	P.Z.SetInt(1)

	var rx secp.FieldVal
	var rxArr [32]byte
	copy(rxArr[:], sig[0:32])
	if overflow := rx.SetBytes(&rxArr); overflow != 0 {
		return false
	}
	var s secp.ModNScalar
	var sArr [32]byte
	copy(sArr[:], sig[32:64])
	if overflow := s.SetBytes(&sArr); overflow != 0 {
		return false
	}

	eHash := taggedHash("BIP0340/challenge", sig[0:32], pxBytes, msg)
	var e secp.ModNScalar
	e.SetBytes(&eHash)

	// R = s*G - e*P.
	var sG, eP, Rj secp.JacobianPoint
	secp.ScalarBaseMultNonConst(&s, &sG)
	e.Negate()
	secp.ScalarMultNonConst(&e, &P, &eP)
	secp.AddNonConst(&sG, &eP, &Rj)
	Rj.ToAffine()

	if Rj.Y.IsOdd() {
		return false
	}
	return Rj.X.Equals(&rx)
}
