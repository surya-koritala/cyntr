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
