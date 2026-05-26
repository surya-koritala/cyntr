package curator

import (
	"database/sql"
	"fmt"
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
	s.mu.Lock()
	defer s.mu.Unlock()

	if inv.Timestamp.IsZero() {
		inv.Timestamp = time.Now().UTC()
	}
	successInt := 0
	if inv.Success {
		successInt = 1
	}
	_, err := s.db.Exec(
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
		return fmt.Errorf("insert invocation: %w", err)
	}
	return nil
}

// ListSkillNames returns every distinct skill_name that has at
// least one invocation row.
func (s *Store) ListSkillNames() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT skill_name FROM invocations ORDER BY skill_name`)
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
func (s *Store) LoadInvocations(skillName string, limit int) ([]Invocation, error) {
	q := `SELECT skill_name, tenant, agent, success, error, duration_ms, timestamp, llm_judge_score
	      FROM invocations
	      WHERE skill_name = ?
	      ORDER BY timestamp DESC`
	args := []any{skillName}
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
