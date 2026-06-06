package federation

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

// Module is the Federation kernel module.
type Module struct {
	localID   string
	bus       *ipc.Bus
	peers     *PeerManager
	sync      *PolicySync
	query     *FederatedQuery
	residency *ResidencyPolicy
	transport Transport
}

// NewModule creates a new Federation module.
func NewModule(localID string) *Module {
	pm := NewPeerManager(localID)
	residency := NewResidencyPolicy()
	return &Module{
		localID:   localID,
		peers:     pm,
		sync:      NewPolicySync(pm),
		query:     NewFederatedQuery(pm, localID, residency),
		residency: residency,
		transport: NewHTTPTransport(),
	}
}

// Residency returns the data-residency policy (for demos and helper code).
func (m *Module) Residency() *ResidencyPolicy { return m.residency }

func (m *Module) Name() string           { return "federation" }
func (m *Module) Dependencies() []string { return nil }

// LocalID returns this node's identifier (exposed for demos/tools).
func (m *Module) LocalID() string { return m.localID }

// Peers returns the underlying peer manager (for demos and helper code).
func (m *Module) Peers() *PeerManager { return m.peers }

// SetTransport overrides the default HTTP transport. Used by tests to
// route delegation requests in-process between two kernels.
func (m *Module) SetTransport(t Transport) {
	if t != nil {
		m.transport = t
	}
}

// AddPeer is a programmatic alternative to the HTTP / IPC API.
// Returns the registered peer for convenience.
func (m *Module) AddPeer(p Peer) Peer {
	m.peers.Add(p)
	return p
}

func (m *Module) Init(ctx context.Context, svc *kernel.Services) error {
	m.bus = svc.Bus
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	m.bus.Handle("federation", "federation.join", m.handleJoin)
	m.bus.Handle("federation", "federation.remove", m.handleRemove)
	m.bus.Handle("federation", "federation.peers", m.handlePeers)
	m.bus.Handle("federation", "federation.sync", m.handleSync)
	m.bus.Handle("federation", "federation.query", m.handleQuery)
	m.bus.Handle("federation", "federation.delegate", m.handleDelegate)
	m.bus.Handle("federation", "federation.delegate.inbound", m.handleDelegateInbound)
	return nil
}

func (m *Module) Stop(ctx context.Context) error { return nil }

func (m *Module) Health(ctx context.Context) kernel.HealthStatus {
	peers := m.peers.List()
	return kernel.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("%d peers, local=%s", len(peers), m.localID),
	}
}

func (m *Module) handleJoin(msg ipc.Message) (ipc.Message, error) {
	peer, ok := msg.Payload.(Peer)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected Peer, got %T", msg.Payload)
	}
	m.peers.Add(peer)
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (m *Module) handleRemove(msg ipc.Message) (ipc.Message, error) {
	name, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string, got %T", msg.Payload)
	}
	m.peers.Remove(name)
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (m *Module) handlePeers(msg ipc.Message) (ipc.Message, error) {
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: m.peers.List()}, nil
}

func (m *Module) handleSync(msg ipc.Message) (ipc.Message, error) {
	syncMsg, ok := msg.Payload.(SyncMessage)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected SyncMessage, got %T", msg.Payload)
	}
	accepted := m.sync.AcceptSync(syncMsg)
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: accepted}, nil
}

func (m *Module) handleQuery(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(FederatedQueryRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected FederatedQueryRequest, got %T", msg.Payload)
	}
	results, err := m.query.Query(req)
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: results}, nil
}

// handleDelegate is invoked locally (e.g., by the delegate_agent tool with a
// `peer` argument) to send a delegation request to a remote node.
func (m *Module) handleDelegate(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(DelegateRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected DelegateRequest, got %T", msg.Payload)
	}
	if req.Peer == "" {
		return ipc.Message{}, fmt.Errorf("delegate: peer is required")
	}
	peer, ok := m.peers.Get(req.Peer)
	if !ok {
		return ipc.Message{}, fmt.Errorf("peer %q not registered", req.Peer)
	}

	// Stamp caller node ID so the remote can record provenance.
	if req.Caller == "" {
		req.Caller = m.localID
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := m.transport.Delegate(ctx, peer, req)
	if err != nil {
		return ipc.Message{}, err
	}
	if resp.Error != "" {
		// The remote-supplied error string may contain peer-internal detail;
		// log it internally and return a generic error to the caller.
		log.Printf("federation: remote peer %q returned delegation error: %s", peer.Name, resp.Error)
		return ipc.Message{}, fmt.Errorf("federation: remote peer delegation failed")
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: resp}, nil
}

// authenticatePeer reports whether secret matches a registered peer's shared
// secret, using a constant-time comparison, and returns the authenticated
// peer's name. An empty secret never matches, so federation inbound fails
// closed when no peer/secret is configured.
func (m *Module) authenticatePeer(secret string) (string, bool) {
	if secret == "" {
		return "", false
	}
	for _, p := range m.peers.List() {
		if p.Secret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(p.Secret)) == 1 {
			return p.Name, true
		}
	}
	return "", false
}

// handleDelegateInbound is invoked when a remote peer asks this node to run
// an agent. It enforces the local policy and dispatches to the local
// agent runtime. The remote node controls its own policy boundary.
func (m *Module) handleDelegateInbound(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(DelegateRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected DelegateRequest, got %T", msg.Payload)
	}

	// Authenticate the calling peer by its shared secret before doing anything
	// else. Fail closed: a missing or unrecognized secret is rejected so an
	// unauthenticated party cannot run agents on this node. The authenticated
	// peer name is derived from the matched secret, NOT from the request body —
	// req.Caller is attacker-controlled and must not be trusted for identity.
	authPeer, ok := m.authenticatePeer(req.Secret)
	if !ok {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: DelegateResponse{
			PeerID: m.localID,
			Agent:  req.Agent,
			Error:  "federation_inbound denied: unauthenticated peer",
		}}, nil
	}

	// Data residency: if this tenant's data is pinned to another node, refuse
	// to run the delegation here. Fail closed — the tenant's data must stay on
	// its home node, so agents elsewhere cannot pull it across the boundary.
	if rerr := m.residency.Check(req.Tenant, m.localID); rerr != nil {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: DelegateResponse{
			PeerID: m.localID,
			Agent:  req.Agent,
			Error:  "federation_inbound denied: data residency restriction",
		}}, nil
	}

	// Policy boundary: ask the local policy engine whether this caller is
	// allowed to invoke `agent.chat` on this tenant/agent. Failing closed
	// when the policy engine is present but rejects is the whole point of
	// federation — the receiving node enforces its own policy.
	policyCtx, policyCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer policyCancel()

	// Identity is derived from the authenticated peer, never from the
	// caller-supplied req.Caller / req.User (those are attacker-controlled).
	federatedIdentity := "federation:" + authPeer

	policyResp, perr := m.bus.Request(policyCtx, ipc.Message{
		Source: "federation", Target: "policy", Topic: "policy.check",
		Payload: policy.CheckRequest{
			Tenant: req.Tenant,
			Action: "federation_inbound",
			Tool:   "delegate_agent",
			Agent:  req.Agent,
			User:   federatedIdentity,
		},
	})
	if perr != nil {
		// Only proceed when there is simply no policy module wired. Any other
		// policy error must fail CLOSED rather than silently allowing the call.
		if !errors.Is(perr, ipc.ErrNoHandler) {
			return ipc.Message{Type: ipc.MessageTypeResponse, Payload: DelegateResponse{
				PeerID: m.localID,
				Agent:  req.Agent,
				Error:  "federation_inbound denied: policy check failed",
			}}, nil
		}
	} else if cr, ok := policyResp.Payload.(policy.CheckResponse); ok {
		// Fail closed: only an explicit Allow proceeds. Deny,
		// RequireApproval, or any unrecognized decision is rejected — a
		// federated caller cannot satisfy an interactive approval, so
		// RequireApproval is treated as not-allowed.
		if cr.Decision != policy.Allow {
			return ipc.Message{Type: ipc.MessageTypeResponse, Payload: DelegateResponse{
				PeerID:   m.localID,
				Agent:    req.Agent,
				Error:    fmt.Sprintf("federation_inbound denied by policy: %s", cr.Reason),
				Decision: cr.Decision.String(),
			}}, nil
		}
	} else {
		// Policy module responded but with an unexpected payload type — we
		// cannot confirm an Allow, so fail closed.
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: DelegateResponse{
			PeerID: m.localID,
			Agent:  req.Agent,
			Error:  "federation_inbound denied: policy check failed",
		}}, nil
	}

	// Dispatch to local agent runtime.
	chatCtx, chatCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer chatCancel()

	chatResp, err := m.bus.Request(chatCtx, ipc.Message{
		Source: "federation", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent:   req.Agent,
			Tenant:  req.Tenant,
			User:    federatedIdentity,
			Message: req.Message,
		},
	})
	if err != nil {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: DelegateResponse{
			PeerID: m.localID,
			Agent:  req.Agent,
			Error:  err.Error(),
		}}, nil
	}

	cr, ok := chatResp.Payload.(agent.ChatResponse)
	if !ok {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: DelegateResponse{
			PeerID: m.localID,
			Agent:  req.Agent,
			Error:  fmt.Sprintf("unexpected chat response type: %T", chatResp.Payload),
		}}, nil
	}

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: DelegateResponse{
		PeerID:   m.localID,
		Agent:    cr.Agent,
		Content:  cr.Content,
		Decision: policy.Allow.String(),
	}}, nil
}
