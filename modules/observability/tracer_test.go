package observability

import (
	"context"
	"testing"
)

// TestTracer_SpanLifecycle exercises a basic span start/end against whatever
// global provider is installed (noop by default in tests). It guards against
// accidental panics in the tracer wrapper and serves as a smoke test of the
// trace API surface we depend on.
func TestTracer_SpanLifecycle(t *testing.T) {
	tr := Tracer("test")
	if tr == nil {
		t.Fatal("Tracer returned nil")
	}

	ctx, span := tr.Start(context.Background(), "test.span")
	if span == nil {
		t.Fatal("Start returned nil span")
	}
	if ctx == nil {
		t.Fatal("Start returned nil context")
	}
	// AddEvent / SetAttributes must not panic on a noop span.
	span.AddEvent("midway")
	span.End()
}
