package audit

import (
	"path/filepath"
	"testing"
	"time"
)

func seedEntries(t *testing.T, w *Writer) {
	t.Helper()
	entries := []Entry{
		{ID: "evt_001", Timestamp: time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC), Tenant: "finance", Principal: Principal{User: "jane@corp.com"}, Action: Action{Type: "tool_call"}},
		{ID: "evt_002", Timestamp: time.Date(2026, 3, 19, 11, 0, 0, 0, time.UTC), Tenant: "finance", Principal: Principal{User: "bob@corp.com"}, Action: Action{Type: "model_call"}},
		{ID: "evt_003", Timestamp: time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC), Tenant: "marketing", Principal: Principal{User: "jane@corp.com"}, Action: Action{Type: "tool_call"}},
	}
	for _, e := range entries {
		if err := w.Write(e); err != nil {
			t.Fatalf("seed %s: %v", e.ID, err)
		}
	}
}

func TestQueryByTenant(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(filepath.Join(dir, "audit.db"), "test", "secret")
	defer w.Close()
	seedEntries(t, w)
	results, err := QueryEntries(w.db, QueryFilter{Tenant: "finance"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
}

func TestQueryByActionType(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(filepath.Join(dir, "audit.db"), "test", "secret")
	defer w.Close()
	seedEntries(t, w)
	results, err := QueryEntries(w.db, QueryFilter{ActionType: "tool_call"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
}

func TestQueryByTimeRange(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(filepath.Join(dir, "audit.db"), "test", "secret")
	defer w.Close()
	seedEntries(t, w)
	results, err := QueryEntries(w.db, QueryFilter{
		Since: time.Date(2026, 3, 19, 10, 30, 0, 0, time.UTC),
		Until: time.Date(2026, 3, 19, 11, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "evt_002" {
		t.Fatalf("expected evt_002, got %s", results[0].ID)
	}
}

func TestQueryWithLimit(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(filepath.Join(dir, "audit.db"), "test", "secret")
	defer w.Close()
	seedEntries(t, w)
	results, err := QueryEntries(w.db, QueryFilter{Limit: 1})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestQueryNoResults(t *testing.T) {
	dir := t.TempDir()
	w, _ := NewWriter(filepath.Join(dir, "audit.db"), "test", "secret")
	defer w.Close()
	seedEntries(t, w)
	results, err := QueryEntries(w.db, QueryFilter{Tenant: "nonexistent"})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0, got %d", len(results))
	}
}
