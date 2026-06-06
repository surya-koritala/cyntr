package skill

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Candidate states.
const (
	CandidatePending  = "pending"
	CandidateApproved = "approved"
	CandidateRejected = "rejected"
)

// CandidateVersion is the version stamped onto skills minted from a candidate.
const CandidateVersion = "0.1.0-candidate"

// IPC topics for the candidate pipeline.
const (
	TopicPropose          = "skill.propose"           // payload: ProposeRequest -> ProposeResult
	TopicCandidates       = "skill.candidates"        // payload: status string ("" = pending) -> []Candidate
	TopicCandidateApprove = "skill.candidate_approve" // payload: int64 id -> "ok"
	TopicCandidateReject  = "skill.candidate_reject"  // payload: RejectRequest -> "ok"
	TopicRollback         = "skill.rollback"          // payload: string name -> "ok"
)

// Candidate is a proposed skill awaiting a decision. Candidates are persisted
// so a proposal made during one run can be reviewed and approved later. They
// are tenant-tagged for provenance and approval routing; activation installs
// into the (global) skill registry, consistent with how skills work today.
type Candidate struct {
	ID           int64        `json:"id"`
	Tenant       string       `json:"tenant"`
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Instructions string       `json:"instructions"` // SKILL.md body
	Capabilities Capabilities `json:"capabilities"`
	SourceAgent  string       `json:"source_agent"`
	Status       string       `json:"status"`
	Reason       string       `json:"reason"` // rejection reason / notes
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

// ProposeRequest is the skill.propose payload.
type ProposeRequest struct {
	Tenant       string
	Name         string
	Description  string
	Instructions string
	Capabilities Capabilities
	SourceAgent  string
}

// ProposeResult is the skill.propose response.
type ProposeResult struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	Activated bool   `json:"activated"` // true if auto-activated (safe caps + policy)
}

// RejectRequest is the skill.candidate_reject payload. Tenant, when set,
// scopes the operation to the caller's tenant: the targeted candidate must
// belong to that tenant or the request is refused.
type RejectRequest struct {
	ID     int64
	Reason string
	Tenant string
}

// ApproveRequest is the tenant-scoped skill.candidate_approve payload. The bare
// int64 id form is still accepted for backward compatibility; callers that can
// identify the authenticated principal should send this form with Tenant set so
// a tenant cannot approve another tenant's candidate.
type ApproveRequest struct {
	ID     int64
	Tenant string
}

// RollbackRequest is the tenant-scoped skill.rollback payload. The bare string
// name form is still accepted for backward compatibility.
type RollbackRequest struct {
	Name   string
	Tenant string
}

// CandidatesQuery is the tenant-scoped skill.candidates payload. The bare
// status string form is still accepted for backward compatibility.
type CandidatesQuery struct {
	Status string
	Tenant string
}

// CandidateStore persists skill candidates in SQLite.
type CandidateStore struct {
	mu sync.Mutex
	db *sql.DB
}

// NewCandidateStore opens (or creates) the candidate store at dbPath.
func NewCandidateStore(dbPath string) (*CandidateStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("skill: open candidate db: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS skill_candidates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			instructions TEXT NOT NULL,
			capabilities TEXT NOT NULL DEFAULT '{}',
			source_agent TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			reason TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("skill: create skill_candidates: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_skill_candidates_status ON skill_candidates(status, id)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("skill: create skill_candidates index: %w", err)
	}
	return &CandidateStore{db: db}, nil
}

// Close closes the database.
func (s *CandidateStore) Close() error { return s.db.Close() }

// Propose persists a new pending candidate and returns its id.
func (s *CandidateStore) Propose(c Candidate) (int64, error) {
	capsJSON, err := json.Marshal(c.Capabilities)
	if err != nil {
		return 0, fmt.Errorf("skill: marshal capabilities: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Unix()
	res, err := s.db.Exec(
		`INSERT INTO skill_candidates (tenant, name, description, instructions, capabilities, source_agent, status, reason, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'pending', '', ?, ?)`,
		c.Tenant, c.Name, c.Description, c.Instructions, string(capsJSON), c.SourceAgent, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("skill: propose: %w", err)
	}
	return res.LastInsertId()
}

// Get returns a candidate by id.
func (s *CandidateStore) Get(id int64) (Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scanOne(s.db.QueryRow(
		`SELECT id, tenant, name, description, instructions, capabilities, source_agent, status, reason, created_at, updated_at
		 FROM skill_candidates WHERE id=?`, id))
}

// List returns candidates with the given status, oldest first. An empty
// status returns pending candidates (the common review case).
func (s *CandidateStore) List(status string) ([]Candidate, error) {
	if status == "" {
		status = CandidatePending
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(
		`SELECT id, tenant, name, description, instructions, capabilities, source_agent, status, reason, created_at, updated_at
		 FROM skill_candidates WHERE status=? ORDER BY id ASC`, status)
	if err != nil {
		return nil, fmt.Errorf("skill: list candidates: %w", err)
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		c, err := s.scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListForTenant returns candidates with the given status scoped to a single
// tenant, oldest first. An empty status returns pending candidates. An empty
// tenant returns candidates across all tenants (operator/system view).
func (s *CandidateStore) ListForTenant(status, tenant string) ([]Candidate, error) {
	if tenant == "" {
		return s.List(status)
	}
	if status == "" {
		status = CandidatePending
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(
		`SELECT id, tenant, name, description, instructions, capabilities, source_agent, status, reason, created_at, updated_at
		 FROM skill_candidates WHERE status=? AND tenant=? ORDER BY id ASC`, status, tenant)
	if err != nil {
		return nil, fmt.Errorf("skill: list candidates: %w", err)
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		c, err := s.scanOne(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// TenantOwnsSkill reports whether the given tenant has any candidate (in any
// status) for a skill of this name. Rollback uses this to confirm a caller
// tenant is permitted to roll back a skill it originated.
func (s *CandidateStore) TenantOwnsSkill(tenant, name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(1) FROM skill_candidates WHERE tenant=? AND name=?`, tenant, name).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("skill: tenant ownership check: %w", err)
	}
	return n > 0, nil
}

// SetStatus updates a candidate's status and reason.
func (s *CandidateStore) SetStatus(id int64, status, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(
		`UPDATE skill_candidates SET status=?, reason=?, updated_at=? WHERE id=?`,
		status, reason, time.Now().UTC().Unix(), id)
	if err != nil {
		return fmt.Errorf("skill: set candidate status: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("skill: candidate %d not found", id)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *CandidateStore) scanOne(row rowScanner) (Candidate, error) {
	var c Candidate
	var capsJSON string
	var created, updated int64
	if err := row.Scan(&c.ID, &c.Tenant, &c.Name, &c.Description, &c.Instructions, &capsJSON,
		&c.SourceAgent, &c.Status, &c.Reason, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return Candidate{}, fmt.Errorf("skill: candidate not found")
		}
		return Candidate{}, err
	}
	_ = json.Unmarshal([]byte(capsJSON), &c.Capabilities)
	c.CreatedAt = time.Unix(created, 0).UTC()
	c.UpdatedAt = time.Unix(updated, 0).UTC()
	return c, nil
}

// toInstalledSkill mints an InstalledSkill from an (approved) candidate. The
// result is indistinguishable from a hand-written skill once in the registry.
func (c Candidate) toInstalledSkill() *InstalledSkill {
	return &InstalledSkill{
		Manifest: SkillManifest{
			Name:         c.Name,
			Version:      CandidateVersion,
			Author:       "agent:" + c.SourceAgent,
			Capabilities: c.Capabilities,
		},
		Instructions: c.Instructions,
	}
}

// validateProposal checks a proposal is well-formed before it is persisted.
func validateProposal(req ProposeRequest) error {
	m := SkillManifest{Name: req.Name, Version: CandidateVersion, Capabilities: req.Capabilities}
	if err := m.Validate(); err != nil {
		return err
	}
	if req.Instructions == "" {
		return fmt.Errorf("skill: proposal instructions are required")
	}
	return nil
}
