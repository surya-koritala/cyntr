package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/netguard"
)

// syncPeerTimeout bounds each outbound policy-sync request so a slow or
// unresponsive peer cannot stall the broadcast indefinitely.
const syncPeerTimeout = 10 * time.Second

// PolicySync handles policy synchronization between peers.
type PolicySync struct {
	pm          *PeerManager
	mu          sync.Mutex
	lastVersion map[string]int // peer:type -> last accepted version
	client      *http.Client
}

// NewPolicySync creates a new policy sync handler.
func NewPolicySync(pm *PeerManager) *PolicySync {
	return &PolicySync{
		pm:          pm,
		lastVersion: make(map[string]int),
		// Peer endpoints are operator/attacker-supplied URLs fetched
		// server-side; use an SSRF-guarded client with a bounded timeout.
		client: netguard.GuardedHTTPClient(syncPeerTimeout),
	}
}

// Broadcast sends a sync message to all peers.
func (ps *PolicySync) Broadcast(msg SyncMessage) error {
	peers := ps.pm.List()

	var firstErr error
	for _, peer := range peers {
		ctx, cancel := context.WithTimeout(context.Background(), syncPeerTimeout)
		err := ps.sendToPeer(ctx, peer, msg)
		cancel()
		if err != nil {
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

func (ps *PolicySync) sendToPeer(ctx context.Context, peer Peer, msg SyncMessage) error {
	if err := netguard.ValidatePublicURL(peer.Endpoint); err != nil {
		return fmt.Errorf("send to %s: endpoint rejected", peer.Name)
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal sync: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, peer.Endpoint+"/federation/sync", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("send to %s: %w", peer.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ps.client.Do(req)
	if err != nil {
		return fmt.Errorf("send to %s: %w", peer.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("peer %s returned %d", peer.Name, resp.StatusCode)
	}

	return nil
}
