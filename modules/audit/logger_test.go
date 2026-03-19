package audit

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func TestLoggerImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Logger)(nil)
}

func TestLoggerWritesViaIPC(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")

	bus := ipc.NewBus()
	defer bus.Close()

	logger := NewLogger(dbPath, "test-instance", "test-secret")
	svc := &kernel.Services{Bus: bus}

	ctx := context.Background()
	if err := logger.Init(ctx, svc); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := logger.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer logger.Stop(ctx)

	bus.Publish(ipc.Message{
		Source: "policy", Target: "*", Type: ipc.MessageTypeEvent, Topic: "audit.write",
		Payload: Entry{
			ID: "evt_ipc_001", Timestamp: time.Now().UTC(), Tenant: "finance",
			Action: Action{Type: "policy_check"}, Policy: PolicyDecision{Decision: "allow"},
		},
	})

	time.Sleep(200 * time.Millisecond)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "audit", Topic: "audit.query",
		Payload: QueryFilter{Tenant: "finance"},
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	entries, ok := resp.Payload.([]Entry)
	if !ok {
		t.Fatalf("expected []Entry, got %T", resp.Payload)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1, got %d", len(entries))
	}
	if entries[0].ID != "evt_ipc_001" {
		t.Fatalf("expected evt_ipc_001, got %s", entries[0].ID)
	}
}

func TestLoggerHealthy(t *testing.T) {
	dir := t.TempDir()
	bus := ipc.NewBus()
	defer bus.Close()
	logger := NewLogger(filepath.Join(dir, "audit.db"), "test", "secret")
	ctx := context.Background()
	logger.Init(ctx, &kernel.Services{Bus: bus})
	logger.Start(ctx)
	defer logger.Stop(ctx)
	health := logger.Health(ctx)
	if !health.Healthy {
		t.Fatalf("expected healthy, got: %s", health.Message)
	}
}
