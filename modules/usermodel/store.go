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
	return &Store{db: db}, nil
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

// Delete removes the profile for (tenant, user). Deleting a missing profile
// is a no-op.
func (s *Store) Delete(tenant, user string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM user_profiles WHERE tenant=? AND user=?", tenant, user)
	return err
}
