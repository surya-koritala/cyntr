package usermodel

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// ErrProfileTooLarge is returned when an upsert exceeds MaxSectionBytes for
// either the profile or preferences section.
var ErrProfileTooLarge = errors.New("usermodel: section exceeds 4KB limit")

// ErrNotFound is returned by Get when no profile exists for the given
// (tenant, user).
var ErrNotFound = errors.New("usermodel: profile not found")

// Store persists per-user profiles to SQLite.
type Store struct {
	mu sync.Mutex
	db *sql.DB
}

// NewStore opens (or creates) a profile database at dbPath.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("usermodel: open db: %w", err)
	}
	// Best-effort WAL — ignore error to stay compatible with in-memory ":memory:".
	db.Exec("PRAGMA journal_mode=WAL")

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS user_profiles (
			tenant TEXT NOT NULL,
			user TEXT NOT NULL,
			profile_md TEXT NOT NULL DEFAULT '',
			preferences_md TEXT NOT NULL DEFAULT '',
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (tenant, user)
		)
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("usermodel: create table: %w", err)
	}

	// Migration v1: add last_distilled_at for the narrative distiller. We use
	// ADD COLUMN guarded by a PRAGMA introspection rather than a separate
	// migration table — this keeps the store self-contained and the column
	// optional for old databases.
	if !columnExists(db, "user_profiles", "last_distilled_at") {
		if _, err := db.Exec(`ALTER TABLE user_profiles ADD COLUMN last_distilled_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			db.Close()
			return nil, fmt.Errorf("usermodel: add last_distilled_at: %w", err)
		}
	}

	// Per-tenant distill opt-in. Default off — explicit row with enabled=1
	// is required to turn distillation on for a tenant.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS tenant_distill_config (
			tenant TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("usermodel: create tenant_distill_config: %w", err)
	}

	// user_activity records short summaries of each chat exchange, scoped to
	// (tenant, user). The distiller pulls the most recent N rows here to
	// build its prompt. We deliberately don't store full message bodies — a
	// brief summary keeps the table small and dodges the PII leak risk of
	// re-persisting raw chat content.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS user_activity (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant TEXT NOT NULL,
			user TEXT NOT NULL,
			summary TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("usermodel: create user_activity: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_activity_tu ON user_activity(tenant, user, created_at)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("usermodel: create user_activity index: %w", err)
	}

	// user_facts holds the structured, evidence-backed model the dialectic
	// distiller maintains: discrete claims about the user, each with a
	// confidence and the session it came from. Facts are revised or retired
	// across sessions rather than overwritten; retired rows are kept
	// (status='retired') for auditability rather than deleted.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS user_facts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant TEXT NOT NULL,
			user TEXT NOT NULL,
			fact TEXT NOT NULL,
			confidence REAL NOT NULL DEFAULT 0.5,
			source_session TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("usermodel: create user_facts: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_facts_tu ON user_facts(tenant, user, status, confidence)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("usermodel: create user_facts index: %w", err)
	}

	return &Store{db: db}, nil
}

// columnExists reports whether the named column is present on the given table.
// Used for idempotent column-add migrations.
func columnExists(db *sql.DB, table, col string) bool {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false
		}
		if name == col {
			return true
		}
	}
	return false
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// Get returns the profile for (tenant, user). Returns ErrNotFound if no
// profile has been written yet.
func (s *Store) Get(tenant, user string) (UserProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p UserProfile
	var updatedUnix int64
	err := s.db.QueryRow(
		"SELECT tenant, user, profile_md, preferences_md, updated_at FROM user_profiles WHERE tenant=? AND user=?",
		tenant, user,
	).Scan(&p.Tenant, &p.User, &p.ProfileMD, &p.PreferencesMD, &updatedUnix)
	if err == sql.ErrNoRows {
		return UserProfile{}, ErrNotFound
	}
	if err != nil {
		return UserProfile{}, err
	}
	p.UpdatedAt = time.Unix(updatedUnix, 0).UTC()
	return p, nil
}

// UpsertProfile sets the profile_md for (tenant, user), creating the row if
// needed and leaving preferences_md unchanged.
func (s *Store) UpsertProfile(tenant, user, md string) error {
	if len(md) > MaxSectionBytes {
		return ErrProfileTooLarge
	}
	return s.upsert(tenant, user, &md, nil)
}

// UpsertPreferences sets the preferences_md for (tenant, user), creating the
// row if needed and leaving profile_md unchanged.
func (s *Store) UpsertPreferences(tenant, user, md string) error {
	if len(md) > MaxSectionBytes {
		return ErrProfileTooLarge
	}
	return s.upsert(tenant, user, nil, &md)
}

// upsert inserts or updates a row. Any nil section is left untouched on
// updates (and defaulted to "" on inserts).
func (s *Store) upsert(tenant, user string, profile, prefs *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Unix()

	// Read existing row to merge non-updated sections.
	var existingProfile, existingPrefs string
	err := s.db.QueryRow(
		"SELECT profile_md, preferences_md FROM user_profiles WHERE tenant=? AND user=?",
		tenant, user,
	).Scan(&existingProfile, &existingPrefs)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	newProfile := existingProfile
	newPrefs := existingPrefs
	if profile != nil {
		newProfile = *profile
	}
	if prefs != nil {
		newPrefs = *prefs
	}

	_, err = s.db.Exec(`
		INSERT INTO user_profiles (tenant, user, profile_md, preferences_md, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(tenant, user) DO UPDATE SET
			profile_md=excluded.profile_md,
			preferences_md=excluded.preferences_md,
			updated_at=excluded.updated_at
	`, tenant, user, newProfile, newPrefs, now)
	return err
}

// MarkDistilled stamps the last-distilled timestamp for (tenant, user) to
// the current UTC time. Creates a profile row with empty markdown if one
// doesn't exist yet so the timestamp survives.
func (s *Store) MarkDistilled(tenant, user string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC().Unix()
	_, err := s.db.Exec(`
		INSERT INTO user_profiles (tenant, user, profile_md, preferences_md, updated_at, last_distilled_at)
		VALUES (?, ?, '', '', ?, ?)
		ON CONFLICT(tenant, user) DO UPDATE SET last_distilled_at=excluded.last_distilled_at
	`, tenant, user, now, now)
	return err
}

// LastDistilledAt returns the Unix timestamp of the last successful distill
// for (tenant, user). Returns 0 if the profile has never been distilled.
func (s *Store) LastDistilledAt(tenant, user string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ts int64
	err := s.db.QueryRow(
		"SELECT COALESCE(last_distilled_at, 0) FROM user_profiles WHERE tenant=? AND user=?",
		tenant, user,
	).Scan(&ts)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return ts, err
}

// SetTenantDistillEnabled toggles per-tenant distillation. Distillation is
// off by default — callers must explicitly enable it for a tenant.
func (s *Store) SetTenantDistillEnabled(tenant string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := 0
	if enabled {
		v = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO tenant_distill_config (tenant, enabled, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(tenant) DO UPDATE SET enabled=excluded.enabled, updated_at=excluded.updated_at
	`, tenant, v, time.Now().UTC().Unix())
	return err
}

// RecordActivity appends a per-(tenant, user) activity summary. Summaries
// over 4 KB are truncated rather than rejected — recording activity is a
// best-effort side-effect of chat and must never fail the chat itself.
func (s *Store) RecordActivity(tenant, user, summary string) error {
	if tenant == "" || user == "" {
		return nil
	}
	if len(summary) > MaxSectionBytes {
		summary = summary[:MaxSectionBytes]
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		"INSERT INTO user_activity (tenant, user, summary, created_at) VALUES (?, ?, ?, ?)",
		tenant, user, summary, time.Now().UTC().Unix(),
	)
	return err
}

// RecentActivity returns up to n most-recent activity summaries for
// (tenant, user), newest first. n <= 0 defaults to 10.
func (s *Store) RecentActivity(tenant, user string, n int) ([]ActivitySummary, error) {
	if n <= 0 {
		n = 10
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(
		"SELECT summary, created_at FROM user_activity WHERE tenant=? AND user=? ORDER BY id DESC LIMIT ?",
		tenant, user, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActivitySummary
	for rows.Next() {
		var a ActivitySummary
		var ts int64
		if err := rows.Scan(&a.Summary, &ts); err != nil {
			return nil, err
		}
		a.CreatedAt = time.Unix(ts, 0).UTC()
		out = append(out, a)
	}
	return out, rows.Err()
}

// ActivityCountSince returns the number of activity rows for (tenant, user)
// recorded at or after sinceUnix.
func (s *Store) ActivityCountSince(tenant, user string, sinceUnix int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM user_activity WHERE tenant=? AND user=? AND created_at >= ?",
		tenant, user, sinceUnix,
	).Scan(&n)
	return n, err
}

// ListActiveUsers returns (tenant, user) pairs that have at least minSessions
// activity rows recorded since sinceUnix. Used by the distiller scheduler
// to skip cold users.
func (s *Store) ListActiveUsers(sinceUnix int64, minSessions int) ([]TenantUser, error) {
	if minSessions <= 0 {
		minSessions = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`
		SELECT tenant, user, COUNT(*) FROM user_activity
		WHERE created_at >= ?
		GROUP BY tenant, user
		HAVING COUNT(*) >= ?
		ORDER BY tenant, user
	`, sinceUnix, minSessions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TenantUser
	for rows.Next() {
		var tu TenantUser
		var count int
		if err := rows.Scan(&tu.Tenant, &tu.User, &count); err != nil {
			return nil, err
		}
		tu.RecentSessions = count
		out = append(out, tu)
	}
	return out, rows.Err()
}

// TenantDistillEnabled reports whether distillation is opted in for the
// given tenant. Defaults to false.
func (s *Store) TenantDistillEnabled(tenant string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var v int
	err := s.db.QueryRow("SELECT enabled FROM tenant_distill_config WHERE tenant=?", tenant).Scan(&v)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return v == 1, nil
}

// Delete removes the profile for (tenant, user). Deleting a missing profile
// is a no-op.
func (s *Store) Delete(tenant, user string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM user_profiles WHERE tenant=? AND user=?", tenant, user)
	return err
}
