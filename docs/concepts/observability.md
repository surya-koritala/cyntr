[Cyntr Docs](../README.md) > Concepts > Observability

# Observability

Cyntr emits OpenTelemetry traces and metrics for the agent runtime, tool calls, the IPC bus, and the HTTP API. Export is opt-in via standard OTel env vars; when none are set, instrumentation runs against no-op providers and adds effectively zero overhead.

## Concept overview

- **Traces.** Every chat turn creates an `agent.chat` span. Each tool call creates a child `tool.call` span. Each IPC request creates an `ipc.request` span. Each HTTP request creates a `http.server` span. Span attributes include `tenant`, `agent`, `user`, `tool`, `status` — enough to slice spend and latency by every meaningful dimension without log-parsing.
- **Metrics.** Counters and histograms for token usage (per provider, per agent, per tenant), tool latency, policy decisions (by outcome), IPC backpressure drops, and HTTP error rates.
- **Logs.** Structured JSON on stdout. Every entry carries a `request_id` that matches the trace ID, so you can pivot between traces and logs without instrumentation.

## Quick enable

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
./cyntr start
```

That's it. Spans and metrics flow to your collector.

## Full reference

The exhaustive list of spans, attributes, and metrics — plus the Prometheus scrape endpoint — is at [docs/observability.md](../observability.md).

## Related

- [Full observability docs](../observability.md)
- [Getting started: Deploy](../getting-started/deploy.md) — observability section of the production checklist.
- [Reference: API — metrics](../reference/api.md)
