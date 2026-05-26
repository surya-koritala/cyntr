package quota

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Store persists tenant quota configs and rolling counters.
//
// The schema is intentionally minimal: a `configs` table for per-tenant config
// and a `counters` table that stores the rolling 24h token total and the daily
// session count keyed by tenant + day-bucket (YYYY-MM-DD UTC).
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// NewStore opens (or creates) a SQLite-backed quota store at the given path.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open quota db: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS configs (
		tenant TEXT PRIMARY KEY,
		json   TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create configs table: %w", err)
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS counters (
		tenant TEXT NOT NULL,
		day    TEXT NOT NULL,
		tokens INTEGER NOT NULL DEFAULT 0,
		sessions INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (tenant, day)
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create counters table: %w", err)
	}

	return &Store{db: db}, nil
}

// SetConfig persists a tenant quota config, replacing any existing entry.
func (s *Store) SetConfig(cfg QuotaConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cfg.Tenant == "" {
		return fmt.Errorf("tenant is required")
	}

	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	_, err = s.db.Exec(`INSERT INTO configs (tenant, json, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(tenant) DO UPDATE SET json=excluded.json, updated_at=excluded.updated_at`,
		cfg.Tenant, string(raw), time.Now().UTC().Format(time.RFC3339))
	return err
}

// GetConfig returns the persisted config for a tenant. When no config has been
// stored, a zero-value QuotaConfig (everything unlimited) is returned with
// `ok == false` so callers can distinguish "not configured" from "explicitly
// unlimited".
func (s *Store) GetConfig(tenant string) (QuotaConfig, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`SELECT json FROM configs WHERE tenant = ?`, tenant)
	var raw string
	if err := row.Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return QuotaConfig{Tenant: tenant}, false, nil
		}
		return QuotaConfig{}, false, err
	}
	var cfg QuotaConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return QuotaConfig{}, false, fmt.Errorf("unmarshal config: %w", err)
	}
	return cfg, true, nil
}

// dayKey returns the canonical UTC day bucket used for daily counters.
func dayKey(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

// AddTokens increments the rolling-day token counter and returns the new total.
func (s *Store) AddTokens(tenant string, amount int64, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	day := dayKey(now)
	if _, err := s.db.Exec(`INSERT INTO counters (tenant, day, tokens, sessions) VALUES (?, ?, ?, 0)
		ON CONFLICT(tenant, day) DO UPDATE SET tokens = tokens + excluded.tokens`,
		tenant, day, amount); err != nil {
		return 0, err
	}
	return s.tokensLocked(tenant, day)
}

// CurrentTokens returns the rolling-day token total for a tenant.
func (s *Store) CurrentTokens(tenant string, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokensLocked(tenant, dayKey(now))
}

func (s *Store) tokensLocked(tenant, day string) (int64, error) {
	row := s.db.QueryRow(`SELECT tokens FROM counters WHERE tenant = ? AND day = ?`, tenant, day)
	var n int64
	if err := row.Scan(&n); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}

// IncrementSessions atomically increments the daily session counter and returns
// the new value.
func (s *Store) IncrementSessions(tenant string, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	day := dayKey(now)
	if _, err := s.db.Exec(`INSERT INTO counters (tenant, day, tokens, sessions) VALUES (?, ?, 0, 1)
		ON CONFLICT(tenant, day) DO UPDATE SET sessions = sessions + 1`,
		tenant, day); err != nil {
		return 0, err
	}
	row := s.db.QueryRow(`SELECT sessions FROM counters WHERE tenant = ? AND day = ?`, tenant, day)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// CurrentSessions returns the session count for a tenant on the current day.
func (s *Store) CurrentSessions(tenant string, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	row := s.db.QueryRow(`SELECT sessions FROM counters WHERE tenant = ? AND day = ?`,
		tenant, dayKey(now))
	var n int64
	if err := row.Scan(&n); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}
