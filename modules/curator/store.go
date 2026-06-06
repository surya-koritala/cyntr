package curator

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps the SQLite database that backs the curator. It is
// safe for concurrent use — writes serialise through the embedded
// mutex, reads rely on SQLite's own concurrency guarantees.
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// NewStore opens (or creates) a SQLite database at the given path
// and migrates the schema. Use an in-memory path (":memory:") for
// tests.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open curator db: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		// in-memory databases reject WAL — that's fine, ignore.
		_ = err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS invocations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			skill_name TEXT NOT NULL,
			tenant TEXT NOT NULL,
			agent TEXT NOT NULL,
			success INTEGER NOT NULL,
			error TEXT,
			duration_ms INTEGER NOT NULL,
			timestamp TEXT NOT NULL,
			llm_judge_score REAL
		);
		CREATE INDEX IF NOT EXISTS idx_invocations_skill ON invocations(skill_name);
		CREATE INDEX IF NOT EXISTS idx_invocations_timestamp ON invocations(timestamp);
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate curator db: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Record persists a single invocation. If the timestamp is zero we
// fill in time.Now() so callers can fire-and-forget without setting
// it explicitly.
func (s *Store) Record(inv Invocation) error {
	_, err := s.RecordID(inv)
	return err
}

// RecordID inserts an invocation and returns the new row id. The
// id is what the LLM judge uses to write its score back to the row
// later. Keeping Record() as the convenience wrapper preserves all
// existing v0 callers (skill_router etc.).
func (s *Store) RecordID(inv Invocation) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if inv.Timestamp.IsZero() {
		inv.Timestamp = time.Now().UTC()
	}
	successInt := 0
	if inv.Success {
		successInt = 1
	}
	res, err := s.db.Exec(
		`INSERT INTO invocations (skill_name, tenant, agent, success, error, duration_ms, timestamp, llm_judge_score)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		inv.SkillName,
		inv.Tenant,
		inv.Agent,
		successInt,
		inv.Error,
		inv.DurationMs,
		inv.Timestamp.UTC().Format(time.RFC3339Nano),
		nullableFloat(inv.LLMJudgeScore),
	)
	if err != nil {
		return 0, fmt.Errorf("insert invocation: %w", err)
	}
	return res.LastInsertId()
}

// SetJudgeScore writes an LLM judge score onto a previously
// recorded invocation. Returns an error if the row doesn't exist.
func (s *Store) SetJudgeScore(invocationID int64, score float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.db.Exec(
		`UPDATE invocations SET llm_judge_score = ? WHERE id = ?`,
		score, invocationID,
	)
	if err != nil {
		return fmt.Errorf("update judge score: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("invocation id %d not found", invocationID)
	}
	return nil
}

// tenantClause returns an extra SQL predicate and arg list that scopes a
// query to a single tenant when one is supplied. Tenant isolation: skill
// telemetry is per-tenant, so every read that the judge/scores/prune logic
// performs must be confined to the caller's tenant. A blank tenant means
// "unscoped" — that is only safe for internal/admin callers and MUST NOT be
// reachable from a tenant-facing request; tenant-facing callers are expected
// to pass their authenticated tenant.
//
// The query methods take the tenant as a trailing variadic so existing
// internal callers compile unchanged while tenant-facing callers (the IPC /
// web handlers) can opt into per-tenant scoping by passing their
// authenticated tenant.
func tenantClause(tenant ...string) (string, []any) {
	if len(tenant) == 0 || tenant[0] == "" {
		return "", nil
	}
	return " AND tenant = ?", []any{tenant[0]}
}

// CountInvocations returns the total invocations on record for a
// skill, optionally scoped to a single tenant. Used by the judge
// rate-limiter to skip skills that haven't accumulated enough calls since
// the last judgment.
func (s *Store) CountInvocations(skillName string, tenant ...string) (int, error) {
	clause, args := tenantClause(tenant...)
	q := `SELECT COUNT(*) FROM invocations WHERE skill_name = ?` + clause
	var n int
	err := s.db.QueryRow(q, append([]any{skillName}, args...)...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count invocations: %w", err)
	}
	return n, nil
}

// CountJudged returns the number of invocations for a skill that
// already have an LLM judge score. Combined with CountInvocations,
// this gives the rate-limiter what it needs.
func (s *Store) CountJudged(skillName string, tenant ...string) (int, error) {
	clause, args := tenantClause(tenant...)
	q := `SELECT COUNT(*) FROM invocations WHERE skill_name = ? AND llm_judge_score IS NOT NULL` + clause
	var n int
	err := s.db.QueryRow(q, append([]any{skillName}, args...)...).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count judged: %w", err)
	}
	return n, nil
}

// RecentFailureSamples returns up to `limit` non-empty error strings
// from the most recent failed invocations of a skill. Used to
// populate the prune report so operators can see *why* a skill is
// failing without diffing logs.
func (s *Store) RecentFailureSamples(skillName string, limit int, tenant ...string) ([]string, error) {
	if limit <= 0 {
		limit = 5
	}
	clause, targs := tenantClause(tenant...)
	q := `SELECT error FROM invocations
		 WHERE skill_name = ? AND success = 0 AND error IS NOT NULL AND error != ''` + clause +
		` ORDER BY timestamp DESC LIMIT ?`
	args := append([]any{skillName}, targs...)
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("recent failures: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListSkillNames returns every distinct skill_name that has at
// least one invocation row, optionally scoped to a single tenant so a
// tenant-facing caller never sees other tenants' skill names.
func (s *Store) ListSkillNames(tenant ...string) ([]string, error) {
	clause, args := tenantClause(tenant...)
	// tenantClause yields " AND tenant = ?"; promote it to the first
	// predicate since this query has no other WHERE clause.
	where := ""
	if clause != "" {
		where = " WHERE" + strings.TrimPrefix(clause, " AND")
	}
	q := `SELECT DISTINCT skill_name FROM invocations` + where + ` ORDER BY skill_name`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// LoadInvocations returns invocations for a single skill, newest
// first. limit <= 0 means "all rows".
func (s *Store) LoadInvocations(skillName string, limit int, tenant ...string) ([]Invocation, error) {
	clause, targs := tenantClause(tenant...)
	q := `SELECT skill_name, tenant, agent, success, error, duration_ms, timestamp, llm_judge_score
	      FROM invocations
	      WHERE skill_name = ?` + clause + `
	      ORDER BY timestamp DESC`
	args := append([]any{skillName}, targs...)
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("load invocations: %w", err)
	}
	defer rows.Close()

	var out []Invocation
	for rows.Next() {
		var inv Invocation
		var successInt int
		var errStr sql.NullString
		var tsStr string
		var judge sql.NullFloat64
		if err := rows.Scan(
			&inv.SkillName, &inv.Tenant, &inv.Agent, &successInt,
			&errStr, &inv.DurationMs, &tsStr, &judge,
		); err != nil {
			return nil, err
		}
		inv.Success = successInt == 1
		if errStr.Valid {
			inv.Error = errStr.String
		}
		if t, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
			inv.Timestamp = t
		}
		if judge.Valid {
			v := judge.Float64
			inv.LLMJudgeScore = &v
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func nullableFloat(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}
