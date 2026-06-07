package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/netguard"
)

// maxDelegateResponseBytes caps how much of a remote peer response we read, so
// a hostile or misbehaving peer cannot exhaust memory.
const maxDelegateResponseBytes = 1 << 20 // 1 MiB

// errDelegateUpstream is the generic error surfaced to API callers when a
// remote peer fails. The verbatim peer response/endpoint is logged internally
// only — it can contain secrets and must not leak to the caller.
var errDelegateUpstream = errors.New("federation: remote peer delegation failed")

// DelegateRequest is the payload for a cross-node agent delegation.
// It is sent from a "caller" node to a remote peer to execute an agent
// chat on the peer's local agent runtime, subject to the peer's policy.
type DelegateRequest struct {
	Peer    string `json:"peer"`    // target peer name (caller-side hint; ignored on remote)
	Tenant  string `json:"tenant"`  // tenant on the remote node
	Agent   string `json:"agent"`   // agent name on the remote node
	User    string `json:"user"`    // caller identity (for audit + policy)
	Message string `json:"message"` // user message to send
	Caller  string `json:"caller"`  // calling node ID (for audit)
	// Secret is the per-peer shared secret, carried in the X-Federation-Secret
	// header (not the JSON body) and set by the inbound HTTP handler. The
	// inbound module verifies it before dispatching.
	Secret string `json:"-"`
}

// DelegateResponse is the result of a cross-node delegation.
type DelegateResponse struct {
	PeerID   string `json:"peer_id"` // node that served the request
	Agent    string `json:"agent"`   // agent that produced the response
	Content  string `json:"content"` // assistant content
	Error    string `json:"error,omitempty"`
	Decision string `json:"decision,omitempty"` // policy decision: allow, deny, require_approval
}

// Transport sends a delegation request to a remote peer and returns the response.
// The default implementation is HTTP; tests can supply an in-process transport.
type Transport interface {
	Delegate(ctx context.Context, peer Peer, req DelegateRequest) (DelegateResponse, error)
}

// HTTPTransport is the default Transport using HTTP POST to /federation/delegate.
type HTTPTransport struct {
	Client *http.Client
}

// NewHTTPTransport returns a Transport that uses HTTP. The client is guarded
// so a peer endpoint (or a redirect from one) cannot be aimed at an internal
// address.
func NewHTTPTransport() *HTTPTransport {
	return &HTTPTransport{Client: netguard.GuardedHTTPClient(30 * time.Second)}
}

// Delegate sends a delegation request over HTTP. Peer.Endpoint is operator/
// attacker-supplied, so it is validated against the SSRF guard before the
// fetch. Remote response bodies and endpoints are never echoed to the caller;
// failures return a generic error and are logged internally only.
func (t *HTTPTransport) Delegate(ctx context.Context, peer Peer, req DelegateRequest) (DelegateResponse, error) {
	if err := netguard.ValidatePublicURL(peer.Endpoint); err != nil {
		log.Printf("federation: delegate peer %q endpoint rejected by SSRF guard: %v", peer.Name, err)
		return DelegateResponse{}, errDelegateUpstream
	}

	body, err := json.Marshal(req)
	if err != nil {
		return DelegateResponse{}, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", peer.Endpoint+"/api/v1/federation/inbound/delegate", bytes.NewReader(body))
	if err != nil {
		return DelegateResponse{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if peer.Secret != "" {
		httpReq.Header.Set("X-Federation-Secret", peer.Secret)
	}

	resp, err := t.Client.Do(httpReq)
	if err != nil {
		log.Printf("federation: delegate to peer %q failed: %v", peer.Name, err)
		return DelegateResponse{}, errDelegateUpstream
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxDelegateResponseBytes))
	if resp.StatusCode >= 400 {
		log.Printf("federation: peer %q returned %d: %s", peer.Name, resp.StatusCode, string(raw))
		return DelegateResponse{}, errDelegateUpstream
	}

	// The server may wrap responses in a standard envelope ({"data": ...}).
	// Try both shapes so this transport works against any cyntr build.
	var envelope struct {
		Data  *DelegateResponse `json:"data"`
		Error *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Data != nil {
		if envelope.Error != nil && envelope.Error.Message != "" {
			log.Printf("federation: peer %q delegation error: %s", peer.Name, envelope.Error.Message)
			return DelegateResponse{}, errDelegateUpstream
		}
		return *envelope.Data, nil
	}

	var out DelegateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		log.Printf("federation: decode response from peer %q failed: %v", peer.Name, err)
		return DelegateResponse{}, errDelegateUpstream
	}

	return out, nil
}
