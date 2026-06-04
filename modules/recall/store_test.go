package recall

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "recall.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func idx(s *Store, t *testing.T, tenant, user, session, role, text string) {
	t.Helper()
	if err := s.Index(IndexedMessage{Tenant: tenant, User: user, Session: session, Role: role, Text: text, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("Index: %v", err)
	}
}

func TestSearchRanksBestMatchFirst(t *testing.T) {
	s := newTestStore(t)
	idx(s, t, "acme", "jane", "s1", "user", "the quarterly budget review meeting")
	idx(s, t, "acme", "jane", "s1", "user", "lunch plans for friday afternoon")
	idx(s, t, "acme", "jane", "s2", "user", "kubernetes pod crashloop backoff")

	got, err := s.Search("acme", "jane", "budget", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 match for 'budget', got %d: %+v", len(got), got)
	}
	if got[0].Session != "s1" || got[0].Role != "user" {
		t.Fatalf("unexpected match: %+v", got[0])
	}

	// Multi-result ordering: the more relevant row should sort first.
	idx(s, t, "acme", "jane", "s3", "user", "budget budget budget numbers")
	got, _ = s.Search("acme", "jane", "budget", 5)
	if len(got) < 2 {
		t.Fatalf("expected >=2 matches, got %d", len(got))
	}
	if got[0].Session != "s3" {
		t.Fatalf("expected the budget-heavy row first, got session %q", got[0].Session)
	}
}

func TestSearchTenantAndUserIsolation(t *testing.T) {
	s := newTestStore(t)
	idx(s, t, "acme", "jane", "s1", "user", "secret roadmap details")
	idx(s, t, "globex", "jane", "s1", "user", "secret roadmap details")
	idx(s, t, "acme", "bob", "s1", "user", "secret roadmap details")

	got, err := s.Search("acme", "jane", "roadmap", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("tenant/user isolation broken: got %d rows, want exactly acme/jane's 1", len(got))
	}

	// A tenant with no matching data sees nothing.
	none, _ := s.Search("initech", "jane", "roadmap", 10)
	if len(none) != 0 {
		t.Fatalf("expected no cross-tenant leakage, got %d", len(none))
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	s := newTestStore(t)
	idx(s, t, "acme", "jane", "s1", "user", "anything")
	got, err := s.Search("acme", "jane", "   !!! ", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty/punctuation-only query should return nothing, got %d", len(got))
	}
}

func TestSessionMessagesChronologicalAndCount(t *testing.T) {
	s := newTestStore(t)
	idx(s, t, "acme", "jane", "s1", "user", "first")
	idx(s, t, "acme", "jane", "s1", "assistant", "second")
	idx(s, t, "acme", "jane", "s1", "user", "third")
	idx(s, t, "acme", "jane", "other", "user", "elsewhere")

	msgs, total, err := s.SessionMessages("acme", "jane", "s1", 50)
	if err != nil {
		t.Fatalf("SessionMessages: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3 (session-scoped)", total)
	}
	if len(msgs) != 3 || msgs[0].Text != "first" || msgs[2].Text != "third" {
		t.Fatalf("messages not in chronological order: %+v", msgs)
	}

	// Limit returns the most recent N, still chronological.
	recent, _, _ := s.SessionMessages("acme", "jane", "s1", 2)
	if len(recent) != 2 || recent[0].Text != "second" || recent[1].Text != "third" {
		t.Fatalf("limited window wrong: %+v", recent)
	}
}

func TestSummaryRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if got, n, _ := s.Summary("acme", "jane", "s1"); got != "" || n != 0 {
		t.Fatalf("missing summary should be empty, got %q/%d", got, n)
	}
	if err := s.UpsertSummary("acme", "jane", "s1", "they discussed budgets", 4); err != nil {
		t.Fatalf("UpsertSummary: %v", err)
	}
	got, n, err := s.Summary("acme", "jane", "s1")
	if err != nil || got != "they discussed budgets" || n != 4 {
		t.Fatalf("summary round-trip wrong: %q/%d/%v", got, n, err)
	}
	// Upsert replaces.
	s.UpsertSummary("acme", "jane", "s1", "updated", 9)
	if got, n, _ := s.Summary("acme", "jane", "s1"); got != "updated" || n != 9 {
		t.Fatalf("summary not replaced: %q/%d", got, n)
	}
}
