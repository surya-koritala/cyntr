# Federation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Federation module — peer-to-peer communication between Cyntr instances for policy sync, cross-site audit queries, and health monitoring, with no central controller.

**Architecture:** The Federation module is a kernel module that manages peer connections. Each peer is identified by name and endpoint. Peers authenticate via shared secrets (mTLS deferred). The module syncs global policies to peers, enables cross-site audit queries by forwarding requests to remote peers via HTTP, and tracks peer health. Policy sync uses most-restrictive-wins conflict resolution with monotonic versioning.

**Tech Stack:** Go 1.22+ stdlib `net/http`. Peer communication over HTTP/JSON.

**Spec:** `docs/superpowers/specs/2026-03-19-cyntr-enterprise-platform-design.md` (Section 2.7)

**Dependencies:** Kernel + Policy Engine + Audit Logger (Plans 1-2).

**Deferred to future plans:**
- mTLS authentication — this plan uses shared secret auth headers. mTLS comes with PKI infrastructure.
- DNS-based peer discovery — this plan uses manual registration.
- Continuous audit replication — this plan supports on-demand federated queries.
- Data residency enforcement — built on this foundation in a later plan.

---

## File Structure

```
modules/federation/
├── types.go               # Peer, SyncMessage, FederatedQuery
├── peer.go                # PeerManager: register, health check, HTTP client
├── sync.go                # PolicySync: propagate policies, conflict resolution
├── query.go               # FederatedAuditQuery: fan-out to peers, merge results
├── module.go              # Federation kernel module: IPC + HTTP server for peer API
├── types_test.go
├── peer_test.go
├── sync_test.go
├── query_test.go
└── module_test.go
```

---

## Chunk 1: Types + Peer Management

### Task 1: Define Federation Types

**Files:**
- Create: `modules/federation/types.go`
- Create: `modules/federation/types_test.go`

- [ ] **Step 1: Write failing test**

Create `modules/federation/types_test.go`:
```go
package federation

import "testing"

func TestPeerKey(t *testing.T) {
	p := Peer{Name: "us-east-1", Endpoint: "https://cyntr-east.corp.com:8443"}
	if p.Name != "us-east-1" {
		t.Fatalf("expected us-east-1, got %q", p.Name)
	}
}

func TestPeerStatusString(t *testing.T) {
	tests := []struct {
		s    PeerStatus
		want string
	}{
		{PeerHealthy, "healthy"},
		{PeerUnreachable, "unreachable"},
		{PeerSyncing, "syncing"},
		{PeerStatus(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("PeerStatus(%d).String() = %q, want %q", int(tt.s), got, tt.want)
		}
	}
}

func TestSyncMessageValidate(t *testing.T) {
	m := SyncMessage{Type: "policy", Version: 1, PeerID: "us-east-1"}
	if m.PeerID != "us-east-1" {
		t.Fatalf("expected peer ID, got %q", m.PeerID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/federation/ -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement types**

Create `modules/federation/types.go`:
```go
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
	PeerID  string        `json:"peer_id"`
	Entries []AuditIndex  `json:"entries"`
	Error   string        `json:"error,omitempty"`
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/federation/ -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add modules/federation/types.go modules/federation/types_test.go
git commit -m "feat(federation): define federation types — Peer, SyncMessage, FederatedQuery"
```

---

### Task 2: Implement Peer Manager

**Files:**
- Create: `modules/federation/peer.go`
- Create: `modules/federation/peer_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/federation/peer_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/federation/ -run TestPeerManager -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement peer manager**

Create `modules/federation/peer.go`:
```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/federation/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/federation/peer.go modules/federation/peer_test.go
git commit -m "feat(federation): implement PeerManager with health checking"
```

---

## Chunk 2: Policy Sync + Federated Audit Query

### Task 3: Implement Policy Sync

**Files:**
- Create: `modules/federation/sync.go`
- Create: `modules/federation/sync_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/federation/sync_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/federation/ -run TestPolicySync -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement policy sync**

Create `modules/federation/sync.go`:
```go
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
	pm             *PeerManager
	mu             sync.Mutex
	lastVersion    map[string]int // peer -> last accepted version
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/federation/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/federation/sync.go modules/federation/sync_test.go
git commit -m "feat(federation): implement policy sync with broadcast and version checking"
```

---

### Task 4: Implement Federated Audit Query

**Files:**
- Create: `modules/federation/query.go`
- Create: `modules/federation/query_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/federation/query_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/federation/ -run TestFederated -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement federated query**

Create `modules/federation/query.go`:
```go
package federation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// FederatedQuery handles cross-site audit queries.
type FederatedQuery struct {
	pm     *PeerManager
	client *http.Client
}

// NewFederatedQuery creates a federated query handler.
func NewFederatedQuery(pm *PeerManager) *FederatedQuery {
	return &FederatedQuery{
		pm: pm,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Query fans out a query to all peers and merges results.
// Returns partial results if some peers are unreachable.
func (fq *FederatedQuery) Query(req FederatedQueryRequest) ([]FederatedQueryResponse, error) {
	peers := fq.pm.List()
	if len(peers) == 0 {
		return []FederatedQueryResponse{}, nil
	}

	var (
		results []FederatedQueryResponse
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	for _, peer := range peers {
		wg.Add(1)
		go func(p Peer) {
			defer wg.Done()

			resp, err := fq.queryPeer(p, req)
			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				results = append(results, FederatedQueryResponse{
					PeerID: p.Name,
					Error:  err.Error(),
				})
			} else {
				results = append(results, resp)
			}
		}(peer)
	}

	wg.Wait()

	// Filter out error-only responses for the return
	var successful []FederatedQueryResponse
	for _, r := range results {
		if r.Error == "" {
			successful = append(successful, r)
		}
	}

	if successful == nil {
		successful = []FederatedQueryResponse{}
	}

	return successful, nil
}

func (fq *FederatedQuery) queryPeer(peer Peer, req FederatedQueryRequest) (FederatedQueryResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return FederatedQueryResponse{}, fmt.Errorf("marshal: %w", err)
	}

	resp, err := fq.client.Post(peer.Endpoint+"/federation/audit/query", "application/json", bytes.NewReader(body))
	if err != nil {
		return FederatedQueryResponse{}, fmt.Errorf("query peer %s: %w", peer.Name, err)
	}
	defer resp.Body.Close()

	var result FederatedQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return FederatedQueryResponse{}, fmt.Errorf("decode response from %s: %w", peer.Name, err)
	}

	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/federation/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add modules/federation/query.go modules/federation/query_test.go
git commit -m "feat(federation): implement federated audit query with fan-out and partial failure handling"
```

---

## Chunk 3: Federation Module + Final Verification

### Task 5: Implement Federation as Kernel Module

**Files:**
- Create: `modules/federation/module.go`
- Create: `modules/federation/module_test.go`

- [ ] **Step 1: Write failing tests**

Create `modules/federation/module_test.go`:
```go
package federation

import (
	"context"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func TestModuleImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Module)(nil)
}

func TestModuleAddPeerViaIPC(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	mod := NewModule("local-node")
	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "federation", Topic: "federation.join",
		Payload: Peer{Name: "east-node", Endpoint: "http://east.corp.com:8443"},
	})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("expected ok, got %v", resp.Payload)
	}
}

func TestModuleListPeersViaIPC(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	mod := NewModule("local-node")
	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "federation", Topic: "federation.join",
		Payload: Peer{Name: "east", Endpoint: "http://east"},
	})
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "federation", Topic: "federation.join",
		Payload: Peer{Name: "west", Endpoint: "http://west"},
	})

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "federation", Topic: "federation.peers",
	})
	if err != nil {
		t.Fatalf("peers: %v", err)
	}

	peers, ok := resp.Payload.([]Peer)
	if !ok {
		t.Fatalf("expected []Peer, got %T", resp.Payload)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2, got %d", len(peers))
	}
}

func TestModuleRemovePeerViaIPC(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	mod := NewModule("local-node")
	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "federation", Topic: "federation.join",
		Payload: Peer{Name: "temp", Endpoint: "http://temp"},
	})

	resp, _ := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "federation", Topic: "federation.remove",
		Payload: "temp",
	})
	if resp.Payload != "ok" {
		t.Fatalf("expected ok, got %v", resp.Payload)
	}
}

func TestModuleHealthy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	mod := NewModule("local")
	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)
	h := mod.Health(ctx)
	if !h.Healthy {
		t.Fatalf("expected healthy: %s", h.Message)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/federation/ -run TestModule -v -count=1`
Expected: FAIL

- [ ] **Step 3: Implement federation module**

Create `modules/federation/module.go`:
```go
package federation

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Module is the Federation kernel module.
type Module struct {
	localID   string
	bus       *ipc.Bus
	peers     *PeerManager
	sync      *PolicySync
	query     *FederatedQuery
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/suryakoritala/Cyntr && go test ./modules/federation/ -v -count=1 -race`
Expected: All PASS

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/suryakoritala/Cyntr && go test ./... -count=1 -race`
Expected: All PASS

- [ ] **Step 6: Run go vet and build**

Run: `cd /Users/suryakoritala/Cyntr && go vet ./... && go build -o cyntr ./cmd/cyntr && ./cyntr version`

- [ ] **Step 7: Commit**

```bash
git add modules/federation/module.go modules/federation/module_test.go
git commit -m "feat(federation): implement Federation kernel module with peer management, sync, and query IPC handlers"
```
