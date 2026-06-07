// Package node implements the Cyntr companion-app node protocol (B10).
//
// A "node" is a companion surface — a phone, tablet, laptop, or a browser tab
// acting as one — that pairs to the gateway over a single WebSocket connection
// and then exchanges typed messages with the runtime. Native macOS / iOS /
// Android apps are explicitly OUT OF SCOPE for this ticket and are deferred to
// separate projects; what lives here is the wire protocol plus a thin reference
// web client (web/static/node.html + node.js) that exercises it.
//
// The protocol has two phases over one connection:
//
//  1. Pairing + auth. The client opens the WebSocket and immediately sends a
//     "pair" frame carrying a short pairing code (the same code semantics as
//     modules/channel/pairing.go). The server validates the code against a
//     tenant-scoped store. A bad or missing code fails closed: the server sends
//     an "error" frame and closes the socket. No further frames are processed
//     until pairing succeeds.
//
//  2. Capability handshake. Once paired, the client sends "hello" listing the
//     capabilities it offers (voice / canvas / camera / screen). The server
//     intersects them with the capabilities the tenant/session permits and
//     replies with "welcome" carrying the negotiated set + the bound session.
//     After that, only frames whose capability was negotiated are accepted;
//     anything else fails closed with an "error" frame.
//
// Every session is tenant-scoped. The tenant is bound at pairing time from the
// validated pairing record and can never be changed by a later frame, so a
// client cannot reach across tenants.
package node

import "fmt"

// Protocol message types (the "type" discriminator on every frame).
const (
	// Client -> server.
	MsgPair   = "pair"   // {code, node, capabilities?} — first frame, authenticates.
	MsgHello  = "hello"  // {capabilities} — offer capabilities for negotiation.
	MsgVoice  = "voice"  // {audio|transcript} — sample voice frame.
	MsgCanvas = "canvas" // {ops|data} — sample canvas frame.
	MsgPing   = "ping"

	// Server -> client.
	MsgWelcome = "welcome" // {session, tenant, capabilities} — negotiation result.
	MsgError   = "error"   // {code, message} — protocol/auth failure (fail closed).
	MsgAck     = "ack"     // {ref} — generic acknowledgement of a client frame.
	MsgPong    = "pong"
)

// Capabilities a node may negotiate. The server only ever grants from this set.
const (
	CapVoice  = "voice"
	CapCanvas = "canvas"
	CapCamera = "camera"
	CapScreen = "screen"
)

// allCapabilities is the universe of grantable capabilities.
var allCapabilities = []string{CapVoice, CapCanvas, CapCamera, CapScreen}

// validCapability reports whether c is a known capability name.
func validCapability(c string) bool {
	for _, k := range allCapabilities {
		if k == c {
			return true
		}
	}
	return false
}

// Frame is the envelope for every message in either direction. Payload-bearing
// fields are optional and only meaningful for their corresponding Type. Tenant
// and Session are populated by the server on outbound frames and are IGNORED on
// inbound frames — the server binds them from the authenticated session so a
// client can never assert its own tenant.
type Frame struct {
	Type string `json:"type"`

	// Pairing / handshake.
	Code         string   `json:"code,omitempty"`         // pair: pairing code
	Node         string   `json:"node,omitempty"`         // pair: client-chosen node id
	Capabilities []string `json:"capabilities,omitempty"` // hello/welcome: capability set

	// Server-bound, read-only to clients.
	Tenant  string `json:"tenant,omitempty"`
	Session string `json:"session,omitempty"`

	// Sample payloads.
	Transcript string `json:"transcript,omitempty"` // voice
	Audio      string `json:"audio,omitempty"`      // voice (base64)
	Canvas     any    `json:"canvas,omitempty"`     // canvas ops/data

	// Errors / acks.
	ErrorCode string `json:"error_code,omitempty"`
	Message   string `json:"message,omitempty"`
	Ref       string `json:"ref,omitempty"` // ack: the type being acked
}

// Error codes returned in MsgError frames.
const (
	ErrBadPairing    = "BAD_PAIRING"    // missing/invalid/wrong-tenant pairing code
	ErrNotPaired     = "NOT_PAIRED"     // a non-pair frame arrived before pairing
	ErrNotNegotiated = "NOT_NEGOTIATED" // a capability frame for an ungranted cap
	ErrProtocol      = "PROTOCOL"       // malformed/unexpected frame
)

// negotiateCapabilities intersects the capabilities a client offers with the
// capabilities the session is allowed to use. Order follows the canonical
// allCapabilities order for determinism, and unknown/duplicate offers are
// dropped. The result is the set the server grants; it is never larger than
// either input.
func negotiateCapabilities(offered, allowed []string) []string {
	allowedSet := map[string]bool{}
	for _, c := range allowed {
		allowedSet[c] = true
	}
	offeredSet := map[string]bool{}
	for _, c := range offered {
		if validCapability(c) {
			offeredSet[c] = true
		}
	}
	var out []string
	for _, c := range allCapabilities {
		if offeredSet[c] && allowedSet[c] {
			out = append(out, c)
		}
	}
	return out
}

// errFrame builds a server error frame.
func errFrame(code, msg string) Frame {
	return Frame{Type: MsgError, ErrorCode: code, Message: msg}
}

// validatePairFrame checks an inbound pair frame is structurally sound before
// the code is checked against the store.
func validatePairFrame(f Frame) error {
	if f.Type != MsgPair {
		return fmt.Errorf("expected %q frame, got %q", MsgPair, f.Type)
	}
	if f.Code == "" {
		return fmt.Errorf("pairing code is required")
	}
	return nil
}
