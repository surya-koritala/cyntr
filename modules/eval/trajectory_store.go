package eval

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"

	_ "modernc.org/sqlite"
)

// TrajectoryStore persists full agent trajectories in a tenant-scoped SQLite
// table. Steps are stored as a JSON blob in the row so the canonical schema
// round-trips losslessly; tenant is a first-class indexed column so isolation
// is enforced by every query, never delegated to the payload.
type TrajectoryStore struct {
	mu sync.Mutex
	db *sql.DB
}

// NewTrajectoryStore opens (or creates) a trajectory store at dbPath.
func NewTrajectoryStore(dbPath string) (*TrajectoryStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open trajectory db: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS trajectories (
			id          TEXT PRIMARY KEY,
			schema      TEXT NOT NULL,
			tenant      TEXT NOT NULL,
			user        TEXT NOT NULL,
			agent       TEXT NOT NULL,
			session     TEXT NOT NULL,
			model       TEXT NOT NULL,
			suite       TEXT NOT NULL DEFAULT '',
			run_id      TEXT NOT NULL DEFAULT '',
			prompt      TEXT NOT NULL,
			steps       TEXT NOT NULL,
			output      TEXT NOT NULL,
			outcome     TEXT NOT NULL,
			tool_calls  INTEGER NOT NULL DEFAULT 0,
			turns       INTEGER NOT NULL DEFAULT 0,
			subagent    INTEGER NOT NULL DEFAULT 0,
			created_at  TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_traj_scope ON trajectories(tenant, agent, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_traj_run ON trajectories(tenant, run_id)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			db.Close()
			return nil, fmt.Errorf("trajectory schema: %w", err)
		}
	}
	return &TrajectoryStore{db: db}, nil
}

// Insert persists one trajectory. Tenant and agent are required. Prompt,
// output, and every step's input/observation are run through the same
// secret/PII filters used on the chat path before they hit disk — so even a
// caller that forgot to sanitize cannot leak secrets into the store.
func (s *TrajectoryStore) Insert(t Trajectory) error {
	if t.Tenant == "" || t.Agent == "" {
		return fmt.Errorf("trajectory: tenant and agent are required")
	}
	if t.ID == "" {
		t.ID = genTrajID()
	}
	if t.Schema == "" {
		t.Schema = TrajectorySchemaRaw
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	t = sanitizeTrajectory(t)

	stepsJSON, err := json.Marshal(t.Steps)
	if err != nil {
		return fmt.Errorf("trajectory: marshal steps: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.Exec(
		`INSERT INTO trajectories
		   (id, schema, tenant, user, agent, session, model, suite, run_id, prompt, steps, output, outcome, tool_calls, turns, subagent, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Schema, t.Tenant, t.User, t.Agent, t.Session, t.Model, t.Suite, t.RunID,
		t.Prompt, string(stepsJSON), t.Output, t.Outcome, t.ToolCalls, t.Turns, boolToInt(t.Subagent),
		t.CreatedAt.UTC().Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("trajectory: insert: %w", err)
	}
	return nil
}

// List returns trajectories for a tenant, newest first. Tenant is mandatory:
// there is no cross-tenant listing path. An optional agent and run_id narrow
// the result. limit <= 0 defaults to 100.
func (s *TrajectoryStore) List(tenant, agentFilter, runID string, limit int) ([]Trajectory, error) {
	if tenant == "" {
		return nil, fmt.Errorf("trajectory: tenant is required")
	}
	if limit <= 0 {
		limit = 100
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	q := `SELECT id, schema, tenant, user, agent, session, model, suite, run_id, prompt, steps, output, outcome, tool_calls, turns, subagent, created_at
	      FROM trajectories WHERE tenant=?`
	args := []any{tenant}
	if agentFilter != "" {
		q += " AND agent=?"
		args = append(args, agentFilter)
	}
	if runID != "" {
		q += " AND run_id=?"
		args = append(args, runID)
	}
	q += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("trajectory: list: %w", err)
	}
	defer rows.Close()

	var out []Trajectory
	for rows.Next() {
		t, err := scanTrajectory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Count returns how many trajectories a tenant has (optionally scoped to a
// run_id). Used by tests and the CLI batch reporter.
func (s *TrajectoryStore) Count(tenant, runID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	var err error
	if runID == "" {
		err = s.db.QueryRow(`SELECT COUNT(*) FROM trajectories WHERE tenant=?`, tenant).Scan(&n)
	} else {
		err = s.db.QueryRow(`SELECT COUNT(*) FROM trajectories WHERE tenant=? AND run_id=?`, tenant, runID).Scan(&n)
	}
	return n, err
}

// Close closes the database.
func (s *TrajectoryStore) Close() error { return s.db.Close() }

func scanTrajectory(rows *sql.Rows) (Trajectory, error) {
	var t Trajectory
	var stepsJSON, createdAt string
	var subagent int
	if err := rows.Scan(
		&t.ID, &t.Schema, &t.Tenant, &t.User, &t.Agent, &t.Session, &t.Model, &t.Suite, &t.RunID,
		&t.Prompt, &stepsJSON, &t.Output, &t.Outcome, &t.ToolCalls, &t.Turns, &subagent, &createdAt,
	); err != nil {
		return Trajectory{}, fmt.Errorf("trajectory: scan: %w", err)
	}
	t.Subagent = subagent != 0
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if stepsJSON != "" {
		if err := json.Unmarshal([]byte(stepsJSON), &t.Steps); err != nil {
			return Trajectory{}, fmt.Errorf("trajectory: unmarshal steps: %w", err)
		}
	}
	return t, nil
}

// sanitizeTrajectory runs every free-text field through the shared secret/PII
// redaction. Centralized so both the store and any offline transform share one
// definition of "scrubbed".
func sanitizeTrajectory(t Trajectory) Trajectory {
	scrub := func(s string) string { return agent.RedactPII(agent.MaskSecrets(s)) }
	t.Prompt = scrub(t.Prompt)
	t.Output = scrub(t.Output)
	for i := range t.Steps {
		t.Steps[i].Input = scrub(t.Steps[i].Input)
		t.Steps[i].Observation = scrub(t.Steps[i].Observation)
	}
	return t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
