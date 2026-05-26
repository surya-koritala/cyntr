package usermodel

import (
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "usermodel.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestUserModelStoreUpsertAndGet(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertProfile("acme", "alice", "# Alice\nLikes terse answers."); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
	if err := s.UpsertPreferences("acme", "alice", "- prefers metric units"); err != nil {
		t.Fatalf("upsert prefs: %v", err)
	}

	p, err := s.Get("acme", "alice")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if p.Tenant != "acme" || p.User != "alice" {
		t.Fatalf("unexpected ids: %+v", p)
	}
	if !strings.Contains(p.ProfileMD, "Likes terse answers") {
		t.Fatalf("profile_md not preserved: %q", p.ProfileMD)
	}
	if !strings.Contains(p.PreferencesMD, "metric units") {
		t.Fatalf("preferences_md not preserved: %q", p.PreferencesMD)
	}
	if p.UpdatedAt.IsZero() {
		t.Fatal("updated_at should be set")
	}
}

func TestUserModelStoreGetMissingReturnsErrNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Get("nope", "nobody"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUserModelStoreUpsertProfilePreservesPreferences(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertPreferences("acme", "alice", "ORIGINAL PREFS"); err != nil {
		t.Fatal(err)
	}
	// Now write only the profile; preferences must survive.
	if err := s.UpsertProfile("acme", "alice", "NEW PROFILE"); err != nil {
		t.Fatal(err)
	}
	p, err := s.Get("acme", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if p.ProfileMD != "NEW PROFILE" {
		t.Fatalf("profile not updated: %q", p.ProfileMD)
	}
	if p.PreferencesMD != "ORIGINAL PREFS" {
		t.Fatalf("preferences clobbered: %q", p.PreferencesMD)
	}
}

func TestUserModelStoreSizeCap(t *testing.T) {
	s := newTestStore(t)
	big := strings.Repeat("x", MaxSectionBytes+1)
	if err := s.UpsertProfile("acme", "alice", big); err != ErrProfileTooLarge {
		t.Fatalf("expected ErrProfileTooLarge for profile, got %v", err)
	}
	if err := s.UpsertPreferences("acme", "alice", big); err != ErrProfileTooLarge {
		t.Fatalf("expected ErrProfileTooLarge for prefs, got %v", err)
	}
	// Exactly at the cap is allowed.
	atCap := strings.Repeat("y", MaxSectionBytes)
	if err := s.UpsertProfile("acme", "alice", atCap); err != nil {
		t.Fatalf("at-cap should succeed: %v", err)
	}
}

func TestUserModelStoreDelete(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertProfile("acme", "alice", "hi"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("acme", "alice"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get("acme", "alice"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	// Deleting again is a no-op.
	if err := s.Delete("acme", "alice"); err != nil {
		t.Fatalf("delete-missing should be no-op: %v", err)
	}
}

func TestUserModelStoreTenantUserIsolation(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertProfile("t1", "u1", "tenant-1 user-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertProfile("t1", "u2", "tenant-1 user-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertProfile("t2", "u1", "tenant-2 user-1"); err != nil {
		t.Fatal(err)
	}
	p, err := s.Get("t1", "u1")
	if err != nil || p.ProfileMD != "tenant-1 user-1" {
		t.Fatalf("isolation broken (t1/u1): %+v err=%v", p, err)
	}
	p, err = s.Get("t2", "u1")
	if err != nil || p.ProfileMD != "tenant-2 user-1" {
		t.Fatalf("isolation broken (t2/u1): %+v err=%v", p, err)
	}
}
