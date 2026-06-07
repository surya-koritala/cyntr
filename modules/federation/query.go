package federation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/netguard"
)

// maxQueryResponseBytes caps how much of a peer audit-query response we read.
const maxQueryResponseBytes = 4 << 20 // 4 MiB

// FederatedQuery handles cross-site audit queries.
type FederatedQuery struct {
	pm        *PeerManager
	localID   string
	residency *ResidencyPolicy
	client    *http.Client
}

// NewFederatedQuery creates a federated query handler. localID and residency
// are used to enforce data-residency: a tenant pinned to another node is not
// fanned out to remote peers, and entries are scoped to the caller tenant.
func NewFederatedQuery(pm *PeerManager, localID string, residency *ResidencyPolicy) *FederatedQuery {
	return &FederatedQuery{
		pm:        pm,
		localID:   localID,
		residency: residency,
		// Peer endpoints are operator/attacker-supplied URLs fetched
		// server-side, so the query client is SSRF-guarded.
		client: netguard.GuardedHTTPClient(10 * time.Second),
	}
}

// Query fans out a query to all peers and merges results.
// Returns partial results if some peers are unreachable.
func (fq *FederatedQuery) Query(req FederatedQueryRequest) ([]FederatedQueryResponse, error) {
	// Tenant scoping is mandatory: a federated audit query must be bound to a
	// caller tenant so results from one tenant can never be merged into
	// another's view.
	if req.Tenant == "" {
		return nil, fmt.Errorf("federated query: tenant is required")
	}

	// Data residency: if this tenant's data is pinned to a specific node, its
	// audit data must not be pulled across the federation boundary. Fan-out is
	// suppressed for residency-locked tenants — audit is served index-only from
	// the home node, never by querying remote peers.
	if fq.residency != nil {
		if node, locked := fq.residency.GetRule(req.Tenant); locked && node != fq.localID {
			return []FederatedQueryResponse{}, nil
		}
	}

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

	// Filter out error-only responses, and scope every merged entry to the
	// caller tenant. A peer may return entries for other tenants (by bug or
	// malice); dropping anything not matching req.Tenant prevents cross-tenant
	// audit leakage.
	var successful []FederatedQueryResponse
	for _, r := range results {
		if r.Error != "" {
			continue
		}
		scoped := make([]AuditIndex, 0, len(r.Entries))
		for _, e := range r.Entries {
			if e.Tenant == req.Tenant {
				scoped = append(scoped, e)
			}
		}
		r.Entries = scoped
		successful = append(successful, r)
	}

	if successful == nil {
		successful = []FederatedQueryResponse{}
	}

	return successful, nil
}

func (fq *FederatedQuery) queryPeer(peer Peer, req FederatedQueryRequest) (FederatedQueryResponse, error) {
	if err := netguard.ValidatePublicURL(peer.Endpoint); err != nil {
		log.Printf("federation: query peer %q endpoint rejected by SSRF guard: %v", peer.Name, err)
		return FederatedQueryResponse{}, fmt.Errorf("query peer %s: endpoint rejected", peer.Name)
	}

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
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxQueryResponseBytes)).Decode(&result); err != nil {
		return FederatedQueryResponse{}, fmt.Errorf("decode response from %s: %w", peer.Name, err)
	}

	return result, nil
}
