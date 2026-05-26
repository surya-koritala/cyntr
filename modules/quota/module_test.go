package quota

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func newTestModule(t *testing.T) (*Module, *ipc.Bus, func()) {
	t.Helper()
	dir := t.TempDir()
	m := New(filepath.Join(dir, "quota.db"))

	bus := ipc.NewBus()
	svc := &kernel.Services{Bus: bus}
	if err := m.Init(context.Background(), svc); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	cleanup := func() {
		m.Stop(context.Background())
		bus.Close()
	}
	return m, bus, cleanup
}

func TestIPCConfigSetGet(t *testing.T) {
	_, bus, cleanup := newTestModule(t)
	defer cleanup()

	cfg := QuotaConfig{Tenant: "acme", TokensPerDay: 999, RequestsPerMinute: 10}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: ModuleName, Topic: TopicConfigSet, Payload: cfg,
	}); err != nil {
		t.Fatalf("config.set: %v", err)
	}

	resp, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: ModuleName, Topic: TopicConfigGet, Payload: "acme",
	})
	if err != nil {
		t.Fatalf("config.get: %v", err)
	}
	got, ok := resp.Payload.(QuotaConfig)
	if !ok {
		t.Fatalf("expected QuotaConfig, got %T", resp.Payload)
	}
	if got != cfg {
		t.Fatalf("roundtrip mismatch: %+v != %+v", got, cfg)
	}
}

func TestIPCCheckTokens(t *testing.T) {
	m, bus, cleanup := newTestModule(t)
	defer cleanup()

	m.enforcer.SetConfig(QuotaConfig{Tenant: "acme", TokensPerDay: 50})
	m.enforcer.RecordTokens("acme", 45)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 5 more should be okay.
	resp, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: ModuleName, Topic: TopicCheck,
		Payload: CheckRequest{Tenant: "acme", Kind: KindTokens, Amount: 5},
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !resp.Payload.(CheckResponse).Allowed {
		t.Fatalf("expected allowed: %+v", resp.Payload)
	}

	// 10 more should breach.
	resp, _ = bus.Request(ctx, ipc.Message{
		Source: "test", Target: ModuleName, Topic: TopicCheck,
		Payload: CheckRequest{Tenant: "acme", Kind: KindTokens, Amount: 10},
	})
	cr := resp.Payload.(CheckResponse)
	if cr.Allowed {
		t.Fatal("expected denial when over cap")
	}
	if cr.Limit != 50 {
		t.Fatalf("expected limit=50, got %d", cr.Limit)
	}
	if cr.Reason == "" {
		t.Error("denial should carry a reason string")
	}
}

func TestIPCRecord(t *testing.T) {
	m, bus, cleanup := newTestModule(t)
	defer cleanup()

	m.enforcer.SetConfig(QuotaConfig{Tenant: "acme", TokensPerDay: 1000})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: ModuleName, Topic: TopicRecord,
		Payload: RecordRequest{Tenant: "acme", Kind: KindTokens, Amount: 250},
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	usage := m.enforcer.CurrentUsage("acme")
	if usage.TokensToday != 250 {
		t.Fatalf("expected 250 tokens recorded, got %d", usage.TokensToday)
	}
}

func TestIPCNoHandlerWhenModuleAbsent(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: ModuleName, Topic: TopicCheck,
		Payload: CheckRequest{Tenant: "anon", Kind: KindTokens, Amount: 1},
	})
	if err != ipc.ErrNoHandler {
		t.Fatalf("expected ErrNoHandler when quota module is not registered, got %v", err)
	}
}

func TestModuleHealth(t *testing.T) {
	m, _, cleanup := newTestModule(t)
	defer cleanup()

	h := m.Health(context.Background())
	if !h.Healthy {
		t.Fatalf("expected healthy, got %+v", h)
	}
	if m.Name() != ModuleName {
		t.Fatalf("name mismatch: %q", m.Name())
	}
	if len(m.Dependencies()) != 0 {
		t.Fatalf("expected zero dependencies")
	}
}
