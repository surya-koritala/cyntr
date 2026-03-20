package federation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPolicySyncBroadcast(t *testing.T) {
	var received []SyncMessage

	// Mock peer servers
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg SyncMessage
		json.NewDecoder(r.Body).Decode(&msg)
		received = append(received, msg)
		w.WriteHeader(200)
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var msg SyncMessage
		json.NewDecoder(r.Body).Decode(&msg)
		received = append(received, msg)
		w.WriteHeader(200)
	}))
	defer server2.Close()

	pm := NewPeerManager("local")
	pm.Add(Peer{Name: "peer-1", Endpoint: server1.URL})
	pm.Add(Peer{Name: "peer-2", Endpoint: server2.URL})

	sync := NewPolicySync(pm)
	err := sync.Broadcast(SyncMessage{
		Type:    "policy",
		Version: 1,
		PeerID:  "local",
		Payload: "new-policy-data",
	})
	if err != nil {
		t.Fatalf("broadcast: %v", err)
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 peers to receive, got %d", len(received))
	}
}

func TestPolicySyncVersionCheck(t *testing.T) {
	sync := NewPolicySync(NewPeerManager("local"))

	// Accept version 1
	accepted := sync.AcceptSync(SyncMessage{Type: "policy", Version: 1, PeerID: "remote"})
	if !accepted {
		t.Fatal("expected version 1 to be accepted")
	}

	// Reject version 1 again (not newer)
	accepted = sync.AcceptSync(SyncMessage{Type: "policy", Version: 1, PeerID: "remote"})
	if accepted {
		t.Fatal("expected duplicate version to be rejected")
	}

	// Accept version 2
	accepted = sync.AcceptSync(SyncMessage{Type: "policy", Version: 2, PeerID: "remote"})
	if !accepted {
		t.Fatal("expected version 2 to be accepted")
	}
}

func TestPolicySyncRejectsOldVersion(t *testing.T) {
	sync := NewPolicySync(NewPeerManager("local"))
	sync.AcceptSync(SyncMessage{Type: "policy", Version: 5, PeerID: "remote"})

	accepted := sync.AcceptSync(SyncMessage{Type: "policy", Version: 3, PeerID: "remote"})
	if accepted {
		t.Fatal("expected old version to be rejected")
	}
}
