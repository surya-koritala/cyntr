package usermodel

import (
	"fmt"
	"time"
)

// FactStatus values.
const (
	FactActive  = "active"
	FactRetired = "retired"
)

// Fact is one structured, evidence-backed claim about a user, maintained by
// the dialectic distiller. Confidence is in [0,1]; SourceSession records the
// session the claim was last derived from.
type Fact struct {
	ID            int64     `json:"id"`
	Tenant        string    `json:"tenant"`
	User          string    `json:"user"`
	Text          string    `json:"fact"`
	Confidence    float64   `json:"confidence"`
	SourceSession string    `json:"source_session"`
	Status        string    `json:"status"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func clampConfidence(c float64) float64 {
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
}

// AddFact inserts a new active fact for (tenant, user) and returns its id.
func (s *Store) AddFact(tenant, user, text string, confidence float64, sourceSession string) (int64, error) {
	if tenant == "" || user == "" || text == "" {
		return 0, fmt.Errorf("usermodel: AddFact requires tenant, user, and text")
	}
	if len(text) > MaxSectionBytes {
		text = text[:MaxSectionBytes]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Unix()
	res, err := s.db.Exec(
		`INSERT INTO user_facts (tenant, user, fact, confidence, source_session, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'active', ?, ?)`,
		tenant, user, text, clampConfidence(confidence), sourceSession, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("usermodel: AddFact: %w", err)
	}
	return res.LastInsertId()
}

// ReviseFact updates the text and/or confidence of an existing active fact,
// scoped to (tenant, user) so an LLM-supplied id can never target another
// user's row. A blank text leaves the existing text unchanged.
func (s *Store) ReviseFact(tenant, user string, id int64, text string, confidence float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Unix()
	var res interface {
		RowsAffected() (int64, error)
	}
	var err error
	if text == "" {
		res, err = s.db.Exec(
			`UPDATE user_facts SET confidence=?, updated_at=? WHERE id=? AND tenant=? AND user=? AND status='active'`,
			clampConfidence(confidence), now, id, tenant, user,
		)
	} else {
		if len(text) > MaxSectionBytes {
			text = text[:MaxSectionBytes]
		}
		res, err = s.db.Exec(
			`UPDATE user_facts SET fact=?, confidence=?, updated_at=? WHERE id=? AND tenant=? AND user=? AND status='active'`,
			text, clampConfidence(confidence), now, id, tenant, user,
		)
	}
	if err != nil {
		return fmt.Errorf("usermodel: ReviseFact: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("usermodel: ReviseFact: no active fact %d for %s/%s", id, tenant, user)
	}
	return nil
}

// RetireFact marks a fact retired (kept for auditability), scoped to
// (tenant, user).
func (s *Store) RetireFact(tenant, user string, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(
		`UPDATE user_facts SET status='retired', updated_at=? WHERE id=? AND tenant=? AND user=? AND status='active'`,
		time.Now().UTC().Unix(), id, tenant, user,
	)
	if err != nil {
		return fmt.Errorf("usermodel: RetireFact: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// No matching active row — a stale or foreign id. Reject so callers
		// (e.g. the dialectic pass) don't count it as a real retirement.
		return fmt.Errorf("usermodel: RetireFact: no active fact %d for %s/%s", id, tenant, user)
	}
	return nil
}

// ActiveFacts returns active facts for (tenant, user), highest confidence
// first. Used by the dialectic prompt and the user_model_read tool.
func (s *Store) ActiveFacts(tenant, user string) ([]Fact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(
		`SELECT id, tenant, user, fact, confidence, source_session, status, updated_at
		 FROM user_facts WHERE tenant=? AND user=? AND status='active'
		 ORDER BY confidence DESC, id ASC`,
		tenant, user,
	)
	if err != nil {
		return nil, fmt.Errorf("usermodel: ActiveFacts: %w", err)
	}
	defer rows.Close()
	var out []Fact
	for rows.Next() {
		var f Fact
		var updated int64
		if err := rows.Scan(&f.ID, &f.Tenant, &f.User, &f.Text, &f.Confidence, &f.SourceSession, &f.Status, &updated); err != nil {
			return nil, err
		}
		f.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, f)
	}
	return out, rows.Err()
}

// CountActiveFacts returns how many active facts (tenant, user) has.
func (s *Store) CountActiveFacts(tenant, user string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM user_facts WHERE tenant=? AND user=? AND status='active'`,
		tenant, user,
	).Scan(&n)
	return n, err
}

// RetireLowestConfidence retires active facts beyond keep, lowest-confidence
// first, and returns how many it retired. This bounds per-user fact growth.
func (s *Store) RetireLowestConfidence(tenant, user string, keep int) (int, error) {
	if keep < 0 {
		keep = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(
		`UPDATE user_facts SET status='retired', updated_at=?
		 WHERE id IN (
		   SELECT id FROM user_facts WHERE tenant=? AND user=? AND status='active'
		   ORDER BY confidence DESC, id ASC LIMIT -1 OFFSET ?
		 )`,
		time.Now().UTC().Unix(), tenant, user, keep,
	)
	if err != nil {
		return 0, fmt.Errorf("usermodel: RetireLowestConfidence: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
