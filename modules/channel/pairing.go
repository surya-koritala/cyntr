package channel

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// DM pairing (B12). Inbound from messaging surfaces is untrusted input. Under
// the "pairing" policy, a message from an unknown (tenant, channel, user) is
// not handed to the agent — the sender gets a short pairing code and an
// operator must approve it (adding the sender to a tenant-scoped allowlist)
// before any further message reaches the agent.

// DM policies.
const (
	DMPairing = "pairing" // unknown senders must be approved (default for untrusted)
	DMOpen    = "open"    // anyone (or the allowFrom list) may message the agent
	DMClosed  = "closed"  // no inbound is processed
)

// PairingStore persists approved senders and pending pairing codes.
type PairingStore struct {
	mu sync.Mutex
	db *sql.DB
}

// NewPairingStore opens (or creates) the pairing store.
func NewPairingStore(dbPath string) (*PairingStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("pairing: open db: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS paired_senders (
			tenant TEXT NOT NULL, channel TEXT NOT NULL, user_id TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (tenant, channel, user_id)
		)`,
		`CREATE TABLE IF NOT EXISTS pending_pairings (
			tenant TEXT NOT NULL, channel TEXT NOT NULL, user_id TEXT NOT NULL,
			code TEXT NOT NULL, created_at INTEGER NOT NULL,
			PRIMARY KEY (tenant, channel, user_id)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			db.Close()
			return nil, fmt.Errorf("pairing: schema: %w", err)
		}
	}
	return &PairingStore{db: db}, nil
}

func (s *PairingStore) Close() error { return s.db.Close() }

// IsPaired reports whether (tenant, channel, user) has been approved.
func (s *PairingStore) IsPaired(tenant, channel, userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	s.db.QueryRow(`SELECT COUNT(*) FROM paired_senders WHERE tenant=? AND channel=? AND user_id=?`,
		tenant, channel, userID).Scan(&n)
	return n > 0
}

// IssueCode creates (or refreshes) a pending pairing code for a sender and
// returns it. Re-issuing for the same sender replaces the prior code.
func (s *PairingStore) IssueCode(tenant, channel, userID string) (string, error) {
	code := pairingCode()
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO pending_pairings (tenant, channel, user_id, code, created_at) VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(tenant, channel, user_id) DO UPDATE SET code=excluded.code, created_at=excluded.created_at`,
		tenant, channel, userID, code, time.Now().Unix())
	if err != nil {
		return "", fmt.Errorf("pairing: issue code: %w", err)
	}
	return code, nil
}

// ApproveCode approves the pending sender matching (tenant, channel, code),
// moving them to the allowlist. Returns the approved user id.
func (s *PairingStore) ApproveCode(tenant, channel, code string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var userID string
	err := s.db.QueryRow(`SELECT user_id FROM pending_pairings WHERE tenant=? AND channel=? AND code=?`,
		tenant, channel, strings.TrimSpace(code)).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("pairing: no pending request for code %q", code)
	}
	if err != nil {
		return "", err
	}
	if err := s.approveLocked(tenant, channel, userID); err != nil {
		return "", err
	}
	return userID, nil
}

// ApproveUser directly allowlists a sender (operator-initiated, no code).
func (s *PairingStore) ApproveUser(tenant, channel, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.approveLocked(tenant, channel, userID)
}

func (s *PairingStore) approveLocked(tenant, channel, userID string) error {
	if _, err := s.db.Exec(
		`INSERT OR IGNORE INTO paired_senders (tenant, channel, user_id, created_at) VALUES (?, ?, ?, ?)`,
		tenant, channel, userID, time.Now().Unix()); err != nil {
		return fmt.Errorf("pairing: approve: %w", err)
	}
	s.db.Exec(`DELETE FROM pending_pairings WHERE tenant=? AND channel=? AND user_id=?`, tenant, channel, userID)
	return nil
}

// PendingPair is one awaiting-approval request.
type PendingPair struct {
	Tenant  string `json:"tenant"`
	Channel string `json:"channel"`
	UserID  string `json:"user_id"`
	Code    string `json:"code"`
}

// ListPending returns awaiting-approval requests for a tenant.
func (s *PairingStore) ListPending(tenant string) ([]PendingPair, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT tenant, channel, user_id, code FROM pending_pairings WHERE tenant=? ORDER BY created_at`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingPair
	for rows.Next() {
		var p PendingPair
		if err := rows.Scan(&p.Tenant, &p.Channel, &p.UserID, &p.Code); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Gate decides whether an inbound message may reach the agent.
type Gate struct {
	store         *PairingStore
	defaultPolicy string
	policies      map[string]string   // channel -> policy
	allowFrom     map[string][]string // channel -> allowed user ids ("*" = any)
}

// NewGate builds a gate. defaultPolicy applies to channels without an explicit
// policy (empty -> DMOpen, preserving legacy behavior).
func NewGate(store *PairingStore, defaultPolicy string) *Gate {
	if defaultPolicy == "" {
		defaultPolicy = DMOpen
	}
	return &Gate{store: store, defaultPolicy: defaultPolicy, policies: map[string]string{}, allowFrom: map[string][]string{}}
}

// SetPolicy sets the DM policy for a channel and its allowFrom list.
func (g *Gate) SetPolicy(channel, policy string, allowFrom []string) {
	g.policies[channel] = policy
	g.allowFrom[channel] = allowFrom
}

func (g *Gate) policyFor(channel string) string {
	if p, ok := g.policies[channel]; ok && p != "" {
		return p
	}
	return g.defaultPolicy
}

func (g *Gate) allowed(channel, userID string) bool {
	for _, u := range g.allowFrom[channel] {
		if u == "*" || u == userID {
			return true
		}
	}
	return false
}

// Check returns whether the message may proceed to the agent. When it may not,
// the returned reply is what should be sent back to the sender instead (a
// pairing code under the pairing policy, a rejection under closed).
func (g *Gate) Check(msg InboundMessage) (bool, string) {
	switch g.policyFor(msg.Channel) {
	case DMClosed:
		return false, "This channel is not accepting messages."
	case DMOpen:
		// Open: allow the explicit allowFrom list, or everyone if it's empty
		// or contains "*".
		if len(g.allowFrom[msg.Channel]) == 0 || g.allowed(msg.Channel, msg.UserID) {
			return true, ""
		}
		return false, "You are not on the allowlist for this channel."
	default: // DMPairing
		if g.store == nil || g.allowed(msg.Channel, msg.UserID) || g.store.IsPaired(msg.Tenant, msg.Channel, msg.UserID) {
			return true, ""
		}
		code, err := g.store.IssueCode(msg.Tenant, msg.Channel, msg.UserID)
		if err != nil {
			return false, "Unable to start pairing. Please try again later."
		}
		return false, fmt.Sprintf("This assistant requires pairing. Your code is %s — ask an operator to approve it.", code)
	}
}

// pairingCode returns a short, unambiguous uppercase code.
func pairingCode() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789" // no I/O/0/1/L
	buf := make([]byte, 6)
	rand.Read(buf)
	var b strings.Builder
	for _, c := range buf {
		b.WriteByte(alphabet[int(c)%len(alphabet)])
	}
	return b.String()
}
