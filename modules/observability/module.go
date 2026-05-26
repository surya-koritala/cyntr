// Package observability wires OpenTelemetry traces + metrics for Cyntr.
//
// The module is opt-in: when OTEL_EXPORTER_OTLP_ENDPOINT is unset we install
// no-op global providers so every Tracer/Meter call across the codebase costs
// effectively nothing. When the endpoint is configured we stand up SDK
// providers with OTLP/HTTP exporters and register them globally so the rest
// of the system (agent runtime, IPC bus, HTTP API) can pick them up via
// otel.Tracer / otel.Meter without taking a dependency on this package.
package observability

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/log"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const (
	// DefaultServiceName is used when OTEL_SERVICE_NAME is unset.
	DefaultServiceName = "cyntr"
	// DefaultSamplerArg is the default sampling ratio (1.0 = always sample).
	DefaultSamplerArg = 1.0
	// shutdownTimeout caps how long Stop will wait for exporters to flush.
	shutdownTimeout = 5 * time.Second
)

var logger = log.Default().WithModule("observability")

// Module configures global OTel providers on kernel start and tears them
// down on stop. It implements kernel.Module.
type Module struct {
	mu             sync.Mutex
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *metric.MeterProvider
	promRegistry   *prometheus.Exporter
	enabled        bool
}

// New constructs an observability module. Configuration is read from env
// vars at Init time so the module is testable without globals.
func New() *Module {
	return &Module{}
}

// Name implements kernel.Module.
func (m *Module) Name() string { return "observability" }

// Dependencies implements kernel.Module. Observability has no module deps
// and is registered first so other modules can configure tracers/meters
// during their own Init.
func (m *Module) Dependencies() []string { return nil }

// Init reads env config and installs global OTel providers.
//
// Env vars:
//   - OTEL_EXPORTER_OTLP_ENDPOINT: e.g. http://localhost:4318. If unset,
//     no-op providers are installed (zero behavior change).
//   - OTEL_SERVICE_NAME: defaults to "cyntr".
//   - OTEL_TRACES_SAMPLER_ARG: float in [0,1], defaults to 1.0 (always-on).
func (m *Module) Init(ctx context.Context, _ *kernel.Services) error {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		// No-op mode: install noop providers so all tracing calls are cheap.
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
		// MeterProvider already defaults to noop globally, but be explicit:
		// we leave the global meter provider untouched (noop by default).
		m.enabled = false
		logger.Info("observability disabled (OTEL_EXPORTER_OTLP_ENDPOINT not set)", nil)
		return nil
	}

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = DefaultServiceName
	}

	samplerArg := DefaultSamplerArg
	if raw := os.Getenv("OTEL_TRACES_SAMPLER_ARG"); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v >= 0 && v <= 1 {
			samplerArg = v
		}
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return fmt.Errorf("observability: build resource: %w", err)
	}

	traceExp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint+"/v1/traces"), otlptracehttp.WithInsecure())
	if err != nil {
		return fmt.Errorf("observability: trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(samplerArg)),
	)

	metricExp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(endpoint+"/v1/metrics"), otlpmetrichttp.WithInsecure())
	if err != nil {
		// Roll back the trace provider we already configured so partial init
		// doesn't leak a live exporter goroutine.
		_ = tp.Shutdown(context.Background())
		return fmt.Errorf("observability: metric exporter: %w", err)
	}

	// Add a Prometheus reader alongside the OTLP one so /api/v1/metrics/prom
	// can scrape the same instruments. Prometheus reader registration is
	// best-effort: if it fails we still want OTLP export to work.
	promExp, promErr := prometheus.New()
	readers := []metric.Option{
		metric.WithReader(metric.NewPeriodicReader(metricExp)),
		metric.WithResource(res),
	}
	if promErr == nil {
		readers = append(readers, metric.WithReader(promExp))
		m.promRegistry = promExp
	} else {
		logger.Warn("prometheus exporter init failed", map[string]any{"error": promErr.Error()})
	}

	mp := metric.NewMeterProvider(readers...)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	m.mu.Lock()
	m.tracerProvider = tp
	m.meterProvider = mp
	m.enabled = true
	m.mu.Unlock()

	// Eagerly create the canonical metric instruments so they are registered
	// with the SDK even if no traffic flows yet — gives Prometheus scrapes a
	// stable surface from the moment the module starts.
	if err := initInstruments(); err != nil {
		logger.Warn("instrument init failed", map[string]any{"error": err.Error()})
	}

	logger.Info("observability enabled", map[string]any{
		"endpoint": endpoint, "service": serviceName, "sampler": samplerArg,
	})
	return nil
}

// Start implements kernel.Module. Nothing to do — providers are live after Init.
func (m *Module) Start(_ context.Context) error { return nil }

// Stop flushes and shuts down providers. Safe to call when disabled.
func (m *Module) Stop(ctx context.Context) error {
	m.mu.Lock()
	tp := m.tracerProvider
	mp := m.meterProvider
	m.mu.Unlock()

	if tp == nil && mp == nil {
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	var firstErr error
	if tp != nil {
		if err := tp.Shutdown(shutdownCtx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("observability: tracer provider shutdown: %w", err)
		}
	}
	if mp != nil {
		if err := mp.Shutdown(shutdownCtx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("observability: meter provider shutdown: %w", err)
		}
	}
	return firstErr
}

// Health implements kernel.Module.
func (m *Module) Health(_ context.Context) kernel.HealthStatus {
	m.mu.Lock()
	enabled := m.enabled
	m.mu.Unlock()
	msg := "disabled (no OTLP endpoint)"
	if enabled {
		msg = "enabled"
	}
	return kernel.HealthStatus{Healthy: true, Message: msg}
}

// Enabled reports whether OTLP export is active. Useful for tests.
func (m *Module) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

// PrometheusHandler returns the http.Handler that serves Prometheus-format
// exposition for the OTel-managed instruments, or nil when the Prometheus
// exporter is not active.
//
// The OTel Prometheus exporter registers itself as a Collector against
// prometheus.DefaultRegisterer, so we return promhttp.HandlerFor on the
// default gatherer — that's the standard pattern from the OTel docs.
func (m *Module) PrometheusHandler() http.Handler {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.promRegistry == nil {
		return nil
	}
	return promhttp.HandlerFor(promclient.DefaultGatherer, promhttp.HandlerOpts{})
}

// MustTracer is a thin convenience that returns a tracer scoped to name
// using the global provider. Equivalent to Tracer(name); kept short because
// instrumentation sites call it a lot.
func MustTracer(name string) trace.Tracer { return Tracer(name) }
