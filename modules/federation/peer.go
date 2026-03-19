package federation

import (
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

// PeerManager manages federation peers.
type PeerManager struct {
	mu      sync.RWMutex
	localID string
	peers   map[string]*Peer
	client  *http.Client
}

// NewPeerManager creates a peer manager for the given local node ID.
func NewPeerManager(localID string) *PeerManager {
	return &PeerManager{
		localID: localID,
		peers:   make(map[string]*Peer),
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Add registers a peer.
func (pm *PeerManager) Add(peer Peer) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.peers[peer.Name] = &peer
}

// Get returns a peer by name.
func (pm *PeerManager) Get(name string) (Peer, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	p, ok := pm.peers[name]
	if !ok {
		return Peer{}, false
	}
	return *p, true
}

// List returns all peers sorted by name.
func (pm *PeerManager) List() []Peer {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	peers := make([]Peer, 0, len(pm.peers))
	for _, p := range pm.peers {
		peers = append(peers, *p)
	}
	sort.Slice(peers, func(i, j int) bool {
		return peers[i].Name < peers[j].Name
	})
	return peers
}

// Remove removes a peer.
func (pm *PeerManager) Remove(name string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.peers, name)
}

// CheckHealth pings a peer's health endpoint and updates status.
func (pm *PeerManager) CheckHealth(name string) error {
	pm.mu.RLock()
	p, ok := pm.peers[name]
	if !ok {
		pm.mu.RUnlock()
		return fmt.Errorf("peer %q not found", name)
	}
	endpoint := p.Endpoint
	pm.mu.RUnlock()

	resp, err := pm.client.Get(endpoint + "/federation/health")
	if err != nil {
		pm.mu.Lock()
		if peer, ok := pm.peers[name]; ok {
			peer.Status = PeerUnreachable
		}
		pm.mu.Unlock()
		return fmt.Errorf("peer %q unreachable: %w", name, err)
	}
	defer resp.Body.Close()

	pm.mu.Lock()
	if peer, ok := pm.peers[name]; ok {
		peer.Status = PeerHealthy
		peer.LastSeen = time.Now()
	}
	pm.mu.Unlock()

	return nil
}

// LocalID returns the local node identifier.
func (pm *PeerManager) LocalID() string {
	return pm.localID
}
