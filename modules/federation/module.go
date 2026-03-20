package federation

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Module is the Federation kernel module.
type Module struct {
	localID string
	bus     *ipc.Bus
	peers   *PeerManager
	sync    *PolicySync
	query   *FederatedQuery
}

// NewModule creates a new Federation module.
func NewModule(localID string) *Module {
	pm := NewPeerManager(localID)
	return &Module{
		localID: localID,
		peers:   pm,
		sync:    NewPolicySync(pm),
		query:   NewFederatedQuery(pm),
	}
}

func (m *Module) Name() string           { return "federation" }
func (m *Module) Dependencies() []string { return nil }

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
