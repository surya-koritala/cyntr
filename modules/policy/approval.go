package policy

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ApprovalStatus represents the state of an approval request.
type ApprovalStatus int

const (
	ApprovalPending ApprovalStatus = iota
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
	ID        string         `json:"id"`
	Tenant    string         `json:"tenant"`
	User      string         `json:"user"`
	Agent     string         `json:"agent"`
	Action    string         `json:"action"`
	Tool      string         `json:"tool"`
	Rule      string         `json:"rule"` // policy rule that triggered approval
	Status    ApprovalStatus `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	ExpiresAt time.Time      `json:"expires_at"`
	DecidedBy string         `json:"decided_by"` // who approved/denied
	DecidedAt time.Time      `json:"decided_at"`
}

// retainAfterTerminal is how long a decided or expired request is kept around
// after its terminal time before being swept. It must comfortably exceed the
// runtime's approval polling window (currently 5 minutes) so a waiter can still
// observe the final status; after that the entry is garbage and is removed to
// keep the requests map bounded.
const retainAfterTerminal = 10 * time.Minute

// ApprovalQueue manages pending approval requests.
type ApprovalQueue struct {
	mu       sync.RWMutex
	requests map[string]*ApprovalRequest
	ttl      time.Duration // how long requests stay pending
	retain   time.Duration // how long terminal requests are retained before sweep
}

// NewApprovalQueue creates an approval queue with the given TTL for pending requests.
func NewApprovalQueue(ttl time.Duration) *ApprovalQueue {
	if ttl == 0 {
		ttl = 15 * time.Minute
	}
	return &ApprovalQueue{
		requests: make(map[string]*ApprovalRequest),
		ttl:      ttl,
		retain:   retainAfterTerminal,
	}
}

// Submit adds a new approval request. Returns the request ID.
func (q *ApprovalQueue) Submit(req ApprovalRequest) string {
	q.mu.Lock()
	defer q.mu.Unlock()

	if req.ID == "" {
		req.ID = newApprovalID()
	}
	req.Status = ApprovalPending
	req.CreatedAt = time.Now().UTC()
	req.ExpiresAt = req.CreatedAt.Add(q.ttl)
	q.requests[req.ID] = &req
	q.sweepLocked(time.Now())
	return req.ID
}

// sweepLocked removes terminal (approved/denied/expired) requests whose
// retention window has elapsed, and lazily expires pending requests that are
// past their deadline. Callers must hold the write lock. This keeps the
// requests map bounded over the lifetime of the process.
func (q *ApprovalQueue) sweepLocked(now time.Time) {
	for id, r := range q.requests {
		if r.Status == ApprovalPending {
			if now.After(r.ExpiresAt) {
				r.Status = ApprovalExpired
				r.DecidedAt = now.UTC()
			}
			continue
		}
		// Terminal state: drop once retention has elapsed. Use DecidedAt when
		// set, otherwise fall back to ExpiresAt so legacy entries still age out.
		terminalAt := r.DecidedAt
		if terminalAt.IsZero() {
			terminalAt = r.ExpiresAt
		}
		if now.After(terminalAt.Add(q.retain)) {
			delete(q.requests, id)
		}
	}
}

// Sweep removes expired/terminal entries. Safe for concurrent use; intended to
// be called periodically by a background sweeper if one is wired up.
func (q *ApprovalQueue) Sweep() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.sweepLocked(time.Now())
}

// newApprovalID returns an unguessable approval identifier. Sequential
// timestamp-based IDs let an attacker enumerate or forge pending approval IDs,
// so we use crypto/rand instead.
func newApprovalID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read effectively never fails; fall back to time-based entropy
		// rather than panicking in a hot path.
		return fmt.Sprintf("apr_%x", time.Now().UnixNano())
	}
	return "apr_" + hex.EncodeToString(b[:])
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
		req.DecidedAt = time.Now().UTC()
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
	// We take the write lock because we mutate request state (lazily expiring
	// pending requests and sweeping terminal ones); doing so under an RLock is
	// a data race.
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	q.sweepLocked(now)

	var pending []ApprovalRequest
	for _, r := range q.requests {
		if r.Status == ApprovalPending {
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
