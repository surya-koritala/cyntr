package audit

import (
	"fmt"
	"testing"
	"time"
)

func TestRotatingWriterCreatesFile(t *testing.T) {
	dir := t.TempDir()
	rw, err := NewRotatingWriter(dir, "test", "secret")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer rw.Close()

	err = rw.Write(Entry{
		ID: "evt_001", Timestamp: time.Now().UTC(), Tenant: "finance",
		Action: Action{Type: "test"},
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestRotatingWriterCurrentPath(t *testing.T) {
	dir := t.TempDir()
	rw, _ := NewRotatingWriter(dir, "test", "secret")
	defer rw.Close()

	path := rw.CurrentPath()
	today := time.Now().UTC().Format("2006-01-02")
	expected := dir + "/audit-" + today + ".db"
	if path != expected {
		t.Fatalf("expected %q, got %q", expected, path)
	}
}

func TestRotatingWriterVerifyChain(t *testing.T) {
	dir := t.TempDir()
	rw, _ := NewRotatingWriter(dir, "test", "secret")
	defer rw.Close()

	for i := 0; i < 3; i++ {
		rw.Write(Entry{
			ID: fmt.Sprintf("evt_%03d", i), Timestamp: time.Now().UTC(),
			Tenant: "t", Action: Action{Type: "test"},
		})
	}

	if err := rw.VerifyChain(); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestRotatingWriterMultipleWrites(t *testing.T) {
	dir := t.TempDir()
	rw, _ := NewRotatingWriter(dir, "test", "secret")
	defer rw.Close()

	// Write 10 entries
	for i := 0; i < 10; i++ {
		rw.Write(Entry{
			ID: fmt.Sprintf("evt_%03d", i), Timestamp: time.Now().UTC(),
			Tenant: "finance", Action: Action{Type: "test"},
		})
	}

	// Verify chain
	if err := rw.VerifyChain(); err != nil {
		t.Fatalf("chain broken: %v", err)
	}
}

func TestRotatingWriterCloseAndReopen(t *testing.T) {
	dir := t.TempDir()

	// Write with first instance
	rw1, _ := NewRotatingWriter(dir, "test", "secret")
	rw1.Write(Entry{ID: "evt_001", Timestamp: time.Now().UTC(), Tenant: "t", Action: Action{Type: "test"}})
	rw1.Close()

	// Reopen — chain should continue
	rw2, _ := NewRotatingWriter(dir, "test", "secret")
	defer rw2.Close()
	rw2.Write(Entry{ID: "evt_002", Timestamp: time.Now().UTC(), Tenant: "t", Action: Action{Type: "test"}})

	if err := rw2.VerifyChain(); err != nil {
		t.Fatalf("chain broken after reopen: %v", err)
	}
}

func TestRotatingWriterDB(t *testing.T) {
	dir := t.TempDir()
	rw, _ := NewRotatingWriter(dir, "test", "secret")
	defer rw.Close()

	db := rw.DB()
	if db == nil {
		t.Fatal("expected non-nil DB")
	}
}
