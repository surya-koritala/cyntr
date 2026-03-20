package policy

import (
	"testing"
	"time"
)

func TestApprovalQueueSubmitAndGet(t *testing.T) {
	q := NewApprovalQueue(15 * time.Minute)
	id := q.Submit(ApprovalRequest{Tenant: "finance", User: "jane", Action: "tool_call", Tool: "shell_exec", Rule: "finance-shell"})
	if id == "" {
		t.Fatal("expected ID")
	}

	req, ok := q.Get(id)
	if !ok {
		t.Fatal("expected found")
	}
	if req.Status != ApprovalPending {
		t.Fatalf("expected pending, got %s", req.Status)
	}
	if req.Tenant != "finance" {
		t.Fatalf("got %q", req.Tenant)
	}
}

func TestApprovalQueueApprove(t *testing.T) {
	q := NewApprovalQueue(15 * time.Minute)
	id := q.Submit(ApprovalRequest{Tenant: "t", Action: "test"})

	if err := q.Approve(id, "admin@corp.com"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	req, _ := q.Get(id)
	if req.Status != ApprovalApproved {
		t.Fatalf("expected approved, got %s", req.Status)
	}
	if req.DecidedBy != "admin@corp.com" {
		t.Fatalf("got %q", req.DecidedBy)
	}
}

func TestApprovalQueueDeny(t *testing.T) {
	q := NewApprovalQueue(15 * time.Minute)
	id := q.Submit(ApprovalRequest{Tenant: "t", Action: "test"})

	q.Deny(id, "admin")
	req, _ := q.Get(id)
	if req.Status != ApprovalDenied {
		t.Fatalf("expected denied, got %s", req.Status)
	}
}

func TestApprovalQueueDoubleDecision(t *testing.T) {
	q := NewApprovalQueue(15 * time.Minute)
	id := q.Submit(ApprovalRequest{})
	q.Approve(id, "admin")

	if err := q.Approve(id, "other"); err == nil {
		t.Fatal("expected error for double decision")
	}
}

func TestApprovalQueueExpiration(t *testing.T) {
	q := NewApprovalQueue(50 * time.Millisecond)
	id := q.Submit(ApprovalRequest{})

	time.Sleep(100 * time.Millisecond)

	if err := q.Approve(id, "admin"); err == nil {
		t.Fatal("expected error for expired approval")
	}
}

func TestApprovalQueueListPending(t *testing.T) {
	q := NewApprovalQueue(15 * time.Minute)
	q.Submit(ApprovalRequest{ID: "a1", Tenant: "t"})
	q.Submit(ApprovalRequest{ID: "a2", Tenant: "t"})

	id3 := q.Submit(ApprovalRequest{ID: "a3", Tenant: "t"})
	q.Approve(id3, "admin") // not pending

	pending := q.ListPending()
	if len(pending) != 2 {
		t.Fatalf("expected 2, got %d", len(pending))
	}
}

func TestApprovalQueueNotFound(t *testing.T) {
	q := NewApprovalQueue(time.Minute)
	if err := q.Approve("nonexistent", "admin"); err == nil {
		t.Fatal("expected error")
	}
}

func TestApprovalQueueCount(t *testing.T) {
	q := NewApprovalQueue(time.Minute)
	q.Submit(ApprovalRequest{ID: "a1"})
	q.Submit(ApprovalRequest{ID: "a2"})
	if q.Count() != 2 {
		t.Fatalf("expected 2, got %d", q.Count())
	}
}

func TestApprovalStatusString(t *testing.T) {
	tests := []struct {
		s    ApprovalStatus
		want string
	}{
		{ApprovalPending, "pending"},
		{ApprovalApproved, "approved"},
		{ApprovalDenied, "denied"},
		{ApprovalExpired, "expired"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}
