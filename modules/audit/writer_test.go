package audit

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestWriterCreateAndWrite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")
	w, err := NewWriter(dbPath, "test-instance", "test-secret")
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()

	entry := Entry{
		ID: "evt_001", Timestamp: time.Now().UTC(), Instance: "test", Tenant: "finance",
		Principal: Principal{User: "jane@corp.com", Agent: "analyst", Role: "user"},
		Action: Action{Type: "tool_call", Module: "runtime", Detail: map[string]string{"tool": "shell"}},
		Policy: PolicyDecision{Rule: "test-rule", Decision: "allow", DecidedBy: "policy_engine"},
		Result: Result{Status: "success", DurationMs: 100},
	}
	if err := w.Write(entry); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestWriterHashChain(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")
	w, err := NewWriter(dbPath, "test-instance", "test-secret")
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()

	for i := 0; i < 3; i++ {
		entry := Entry{
			ID: fmt.Sprintf("evt_%03d", i), Timestamp: time.Now().UTC(),
			Tenant: "finance", Action: Action{Type: "test"},
		}
		if err := w.Write(entry); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := w.VerifyChain(); err != nil {
		t.Fatalf("chain verification failed: %v", err)
	}
}

func TestWriterChainDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")
	w, err := NewWriter(dbPath, "test-instance", "test-secret")
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}

	for i := 0; i < 3; i++ {
		w.Write(Entry{
			ID: fmt.Sprintf("evt_%03d", i), Timestamp: time.Now().UTC(),
			Tenant: "finance", Action: Action{Type: "test"},
		})
	}

	// Tamper with the data column to break the hash chain
	_, err = w.db.Exec("UPDATE audit_entries SET data = replace(data, 'finance', 'hacked') WHERE id = 'evt_001'")
	if err != nil {
		t.Fatalf("tamper: %v", err)
	}

	err = w.VerifyChain()
	if err == nil {
		t.Fatal("expected chain verification to detect tampering")
	}
}

func TestWriterAppendOnly(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")
	w, err := NewWriter(dbPath, "test-instance", "test-secret")
	if err != nil {
		t.Fatalf("new writer: %v", err)
	}
	defer w.Close()

	w.Write(Entry{ID: "evt_001", Timestamp: time.Now().UTC(), Tenant: "t", Action: Action{Type: "test"}})

	var count int
	w.db.QueryRow("SELECT COUNT(*) FROM audit_entries").Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 entry, got %d", count)
	}
}
