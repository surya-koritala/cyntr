package federation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPeerManagerAddAndGet(t *testing.T) {
	pm := NewPeerManager("local-node")

	pm.Add(Peer{Name: "us-east-1", Endpoint: "https://east.corp.com:8443", Secret: "shared-secret"})

	peer, ok := pm.Get("us-east-1")
	if !ok {
		t.Fatal("expected peer")
	}
	if peer.Endpoint != "https://east.corp.com:8443" {
		t.Fatalf("expected endpoint, got %q", peer.Endpoint)
	}
}

func TestPeerManagerGetNotFound(t *testing.T) {
	pm := NewPeerManager("local")
	_, ok := pm.Get("nonexistent")
	if ok {
		t.Fatal("expected not found")
	}
}

func TestPeerManagerList(t *testing.T) {
	pm := NewPeerManager("local")
	pm.Add(Peer{Name: "b-node", Endpoint: "http://b"})
	pm.Add(Peer{Name: "a-node", Endpoint: "http://a"})

	peers := pm.List()
	if len(peers) != 2 {
		t.Fatalf("expected 2, got %d", len(peers))
	}
	if peers[0].Name != "a-node" {
		t.Fatalf("expected sorted, got %q first", peers[0].Name)
	}
}

func TestPeerManagerRemove(t *testing.T) {
	pm := NewPeerManager("local")
	pm.Add(Peer{Name: "temp", Endpoint: "http://temp"})
	pm.Remove("temp")

	_, ok := pm.Get("temp")
	if ok {
		t.Fatal("expected removed")
	}
}

func TestPeerManagerCheckHealth(t *testing.T) {
	// Create a mock peer server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	pm := NewPeerManager("local")
	pm.Add(Peer{Name: "test-peer", Endpoint: server.URL})

	err := pm.CheckHealth("test-peer")
	if err != nil {
		t.Fatalf("health check: %v", err)
	}

	peer, _ := pm.Get("test-peer")
	if peer.Status != PeerHealthy {
		t.Fatalf("expected healthy, got %s", peer.Status)
	}
}

func TestPeerManagerCheckHealthUnreachable(t *testing.T) {
	pm := NewPeerManager("local")
	pm.Add(Peer{Name: "dead-peer", Endpoint: "http://127.0.0.1:1"})

	err := pm.CheckHealth("dead-peer")
	if err == nil {
		t.Fatal("expected error for unreachable peer")
	}

	peer, _ := pm.Get("dead-peer")
	if peer.Status != PeerUnreachable {
		t.Fatalf("expected unreachable, got %s", peer.Status)
	}
}
