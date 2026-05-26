package observability

import (
	"context"
	"testing"

	"github.com/cyntr-dev/cyntr/kernel"
)

// TestModule_NoExportPath_ZeroBehaviorChange verifies that when
// OTEL_EXPORTER_OTLP_ENDPOINT is unset (the default for existing deployments)
// Init returns nil, Stop returns nil, no providers get created, and the
// module reports itself as disabled. This is the "no behavior change"
// contract called out in the F12 spec.
func TestModule_NoExportPath_ZeroBehaviorChange(t *testing.T) {
	// Ensure env is clean for the duration of the test.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	m := New()
	ctx := context.Background()

	if err := m.Init(ctx, &kernel.Services{}); err != nil {
		t.Fatalf("Init with unset endpoint returned error: %v", err)
	}
	if m.Enabled() {
		t.Fatal("Enabled() should be false when endpoint is unset")
	}
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop returned error in disabled mode: %v", err)
	}

	// Re-stopping should also be a no-op (idempotent / safe).
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop second call: %v", err)
	}

	// Health should still report healthy.
	if h := m.Health(ctx); !h.Healthy {
		t.Fatalf("Health unhealthy when disabled: %+v", h)
	}
}

// TestModule_LifecycleNames covers the trivial Module interface bits so we
// don't accidentally rename them without updating callers.
func TestModule_LifecycleNames(t *testing.T) {
	m := New()
	if m.Name() != "observability" {
		t.Errorf("Name = %q, want observability", m.Name())
	}
	if deps := m.Dependencies(); len(deps) != 0 {
		t.Errorf("Dependencies should be empty, got %v", deps)
	}
}
