package observability

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

// Tracer returns a Tracer scoped to the given instrumentation name. When the
// observability module is disabled (no OTLP endpoint), this returns a tracer
// backed by the global no-op provider, so callers can use it unconditionally.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
