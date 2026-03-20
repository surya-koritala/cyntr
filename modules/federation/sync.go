package federation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// PolicySync handles policy synchronization between peers.
type PolicySync struct {
	pm          *PeerManager
	mu          sync.Mutex
	lastVersion map[string]int // peer:type -> last accepted version
}

// NewPolicySync creates a new policy sync handler.
func NewPolicySync(pm *PeerManager) *PolicySync {
	return &PolicySync{
		pm:          pm,
		lastVersion: make(map[string]int),
	}
}

// Broadcast sends a sync message to all peers.
func (ps *PolicySync) Broadcast(msg SyncMessage) error {
	peers := ps.pm.List()

	var firstErr error
	for _, peer := range peers {
		if err := ps.sendToPeer(peer, msg); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

// AcceptSync checks if a sync message should be accepted based on version.
// Returns true if accepted (version is newer), false if rejected.
func (ps *PolicySync) AcceptSync(msg SyncMessage) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	key := msg.PeerID + ":" + msg.Type
	lastVer, exists := ps.lastVersion[key]

	if exists && msg.Version <= lastVer {
		return false
	}

	ps.lastVersion[key] = msg.Version
	return true
}

// LastVersion returns the last accepted version for a peer+type combination.
func (ps *PolicySync) LastVersion(peerID, syncType string) int {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.lastVersion[peerID+":"+syncType]
}

func (ps *PolicySync) sendToPeer(peer Peer, msg SyncMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal sync: %w", err)
	}

	resp, err := http.Post(peer.Endpoint+"/federation/sync", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send to %s: %w", peer.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("peer %s returned %d", peer.Name, resp.StatusCode)
	}

	return nil
}
