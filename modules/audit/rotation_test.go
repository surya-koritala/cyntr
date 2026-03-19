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
