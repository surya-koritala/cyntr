package policy

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// ApprovalStatus represents the state of an approval request.
type ApprovalStatus int

const (
	ApprovalPending  ApprovalStatus = iota
	ApprovalApproved
	ApprovalDenied
	ApprovalExpired
)

func (s ApprovalStatus) String() string {
	switch s {
	case ApprovalPending:
		return "pending"
	case ApprovalApproved:
		return "approved"
	case ApprovalDenied:
		return "denied"
	case ApprovalExpired:
		return "expired"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// ApprovalRequest represents a pending action waiting for approval.
type ApprovalRequest struct {
	ID        string
	Tenant    string
	User      string
	Agent     string
	Action    string
	Tool      string
	Rule      string         // policy rule that triggered approval
	Status    ApprovalStatus
	CreatedAt time.Time
	ExpiresAt time.Time
	DecidedBy string         // who approved/denied
	DecidedAt time.Time
}

// ApprovalQueue manages pending approval requests.
type ApprovalQueue struct {
	mu       sync.RWMutex
	requests map[string]*ApprovalRequest
	ttl      time.Duration // how long requests stay pending
}

// NewApprovalQueue creates an approval queue with the given TTL for pending requests.
func NewApprovalQueue(ttl time.Duration) *ApprovalQueue {
	if ttl == 0 {
		ttl = 15 * time.Minute
	}
	return &ApprovalQueue{
		requests: make(map[string]*ApprovalRequest),
		ttl:      ttl,
	}
}

// Submit adds a new approval request. Returns the request ID.
func (q *ApprovalQueue) Submit(req ApprovalRequest) string {
	q.mu.Lock()
	defer q.mu.Unlock()

	if req.ID == "" {
		req.ID = fmt.Sprintf("apr_%d", time.Now().UnixNano())
	}
	req.Status = ApprovalPending
	req.CreatedAt = time.Now().UTC()
	req.ExpiresAt = req.CreatedAt.Add(q.ttl)
	q.requests[req.ID] = &req
	return req.ID
}

// Approve marks a request as approved.
func (q *ApprovalQueue) Approve(id, decidedBy string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	req, ok := q.requests[id]
	if !ok {
		return fmt.Errorf("approval %q not found", id)
	}
	if req.Status != ApprovalPending {
		return fmt.Errorf("approval %q already %s", id, req.Status)
	}
	if time.Now().After(req.ExpiresAt) {
		req.Status = ApprovalExpired
		return fmt.Errorf("approval %q expired", id)
	}

	req.Status = ApprovalApproved
	req.DecidedBy = decidedBy
	req.DecidedAt = time.Now().UTC()
	return nil
}

// Deny marks a request as denied.
func (q *ApprovalQueue) Deny(id, decidedBy string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	req, ok := q.requests[id]
	if !ok {
		return fmt.Errorf("approval %q not found", id)
	}
	if req.Status != ApprovalPending {
		return fmt.Errorf("approval %q already %s", id, req.Status)
	}

	req.Status = ApprovalDenied
	req.DecidedBy = decidedBy
	req.DecidedAt = time.Now().UTC()
	return nil
}

// Get returns an approval request by ID.
func (q *ApprovalQueue) Get(id string) (*ApprovalRequest, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	r, ok := q.requests[id]
	if !ok {
		return nil, false
	}
	return r, true
}

// ListPending returns all pending requests sorted by creation time.
func (q *ApprovalQueue) ListPending() []ApprovalRequest {
	q.mu.RLock()
	defer q.mu.RUnlock()

	now := time.Now()
	var pending []ApprovalRequest
	for _, r := range q.requests {
		if r.Status == ApprovalPending {
			if now.After(r.ExpiresAt) {
				r.Status = ApprovalExpired
				continue
			}
			pending = append(pending, *r)
		}
	}

	sort.Slice(pending, func(i, j int) bool {
		return pending[i].CreatedAt.Before(pending[j].CreatedAt)
	})
	return pending
}

// Count returns the number of pending requests.
func (q *ApprovalQueue) Count() int {
	return len(q.ListPending())
}
