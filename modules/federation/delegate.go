package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

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

// NewHTTPTransport returns a Transport that uses HTTP.
func NewHTTPTransport() *HTTPTransport {
	return &HTTPTransport{Client: &http.Client{Timeout: 30 * time.Second}}
}

// Delegate sends a delegation request over HTTP.
func (t *HTTPTransport) Delegate(ctx context.Context, peer Peer, req DelegateRequest) (DelegateResponse, error) {
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
		return DelegateResponse{}, fmt.Errorf("delegate to peer %s: %w", peer.Name, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return DelegateResponse{}, fmt.Errorf("peer %s returned %d: %s", peer.Name, resp.StatusCode, string(raw))
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
			return DelegateResponse{}, fmt.Errorf("peer %s: %s", peer.Name, envelope.Error.Message)
		}
		return *envelope.Data, nil
	}

	var out DelegateResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return DelegateResponse{}, fmt.Errorf("decode response from %s: %w", peer.Name, err)
	}

	return out, nil
}
