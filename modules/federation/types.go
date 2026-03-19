package federation

import (
	"fmt"
	"time"
)

// PeerStatus represents the connection state of a federation peer.
type PeerStatus int

const (
	PeerHealthy     PeerStatus = iota
	PeerUnreachable
	PeerSyncing
)

func (s PeerStatus) String() string {
	switch s {
	case PeerHealthy:
		return "healthy"
	case PeerUnreachable:
		return "unreachable"
	case PeerSyncing:
		return "syncing"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// Peer represents a federation peer.
type Peer struct {
	Name     string     `json:"name"`
	Endpoint string     `json:"endpoint"`
	Secret   string     `json:"-"` // shared secret for auth
	Status   PeerStatus `json:"status"`
	LastSeen time.Time  `json:"last_seen"`
}

// SyncMessage is sent between peers for policy synchronization.
type SyncMessage struct {
	Type    string `json:"type"`    // "policy"
	Version int    `json:"version"` // monotonic version number
	PeerID  string `json:"peer_id"` // originating peer
	Payload any    `json:"payload"` // the data being synced
}

// FederatedQueryRequest is sent to peers for cross-site audit queries.
type FederatedQueryRequest struct {
	Tenant     string    `json:"tenant"`
	ActionType string    `json:"action_type"`
	Since      time.Time `json:"since"`
	Until      time.Time `json:"until"`
	Limit      int       `json:"limit"`
}

// FederatedQueryResponse is returned by peers for audit queries.
type FederatedQueryResponse struct {
	PeerID  string       `json:"peer_id"`
	Entries []AuditIndex `json:"entries"`
	Error   string       `json:"error,omitempty"`
}

// AuditIndex is a lightweight audit entry reference (not the full entry).
type AuditIndex struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Tenant    string    `json:"tenant"`
	Action    string    `json:"action"`
	User      string    `json:"user"`
	Decision  string    `json:"decision"`
}
