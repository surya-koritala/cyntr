package federation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFederatedQueryFanOut(t *testing.T) {
	// Mock peers that return audit results
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(FederatedQueryResponse{
			PeerID: "peer-1",
			Entries: []AuditIndex{
				{ID: "evt_001", Tenant: "finance", Action: "tool_call", Timestamp: time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC)},
			},
		})
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(FederatedQueryResponse{
			PeerID: "peer-2",
			Entries: []AuditIndex{
				{ID: "evt_002", Tenant: "finance", Action: "model_call", Timestamp: time.Date(2026, 3, 19, 11, 0, 0, 0, time.UTC)},
			},
		})
	}))
	defer server2.Close()

	pm := NewPeerManager("local")
	pm.Add(Peer{Name: "peer-1", Endpoint: server1.URL})
	pm.Add(Peer{Name: "peer-2", Endpoint: server2.URL})

	fq := NewFederatedQuery(pm)
	results, err := fq.Query(FederatedQueryRequest{Tenant: "finance"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestFederatedQueryPartialFailure(t *testing.T) {
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(FederatedQueryResponse{
			PeerID:  "peer-1",
			Entries: []AuditIndex{{ID: "evt_001", Tenant: "finance"}},
		})
	}))
	defer server1.Close()

	pm := NewPeerManager("local")
	pm.Add(Peer{Name: "peer-1", Endpoint: server1.URL})
	pm.Add(Peer{Name: "dead-peer", Endpoint: "http://127.0.0.1:1"})

	fq := NewFederatedQuery(pm)
	results, err := fq.Query(FederatedQueryRequest{Tenant: "finance"})

	// Should return partial results, not fail completely
	if err != nil {
		t.Fatalf("expected partial success, got error: %v", err)
	}
	if len(results) < 1 {
		t.Fatal("expected at least 1 result from healthy peer")
	}
}

func TestFederatedQueryNoPeers(t *testing.T) {
	pm := NewPeerManager("local")
	fq := NewFederatedQuery(pm)

	results, err := fq.Query(FederatedQueryRequest{Tenant: "finance"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}
