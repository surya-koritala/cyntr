# Observability (F12)

Cyntr emits OpenTelemetry traces and metrics for the agent runtime, tool
calls, IPC bus, and HTTP API. Export is opt-in via standard OTel environment
variables — when none are set, instrumentation runs against a no-op provider
and adds effectively zero overhead.

## Enable

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_SERVICE_NAME=cyntr           # optional, default "cyntr"
export OTEL_TRACES_SAMPLER_ARG=1.0       # optional, default 1.0 (always-on)
./cyntr start
```

- `OTEL_EXPORTER_OTLP_ENDPOINT` is the base URL of an OTLP/HTTP collector.
  Cyntr appends `/v1/traces` and `/v1/metrics`.
- When unset, **no exporter is started**. The global tracer/meter providers
  remain no-ops and existing deployments see zero behavior change.

## What's exported

### Spans

| Span name      | Where                              | Attributes                          |
| -------------- | ---------------------------------- | ----------------------------------- |
| `agent.chat`   | `modules/agent/runtime.handleChat` | `tenant`, `agent`, `user`, `turns`  |
| `tool.call`    | per tool invocation in the runtime | `tool`, `agent`, `status`           |
| `ipc.request`  | `bus.Request` calls in the runtime | `target`, `topic`                   |
| `http.request` | every API request                  | `method`, `path`, `status_code`     |

### Metrics

| Metric                          | Type      | Labels                              |
| ------------------------------- | --------- | ----------------------------------- |
| `cyntr.agent.chat.requests`     | counter   | `tenant`, `agent`, `status`         |
| `cyntr.agent.chat.duration_ms`  | histogram | `tenant`, `agent`                   |
| `cyntr.tool.calls`              | counter   | `tenant`, `agent`, `tool`, `status` |
| `cyntr.tool.duration_ms`        | histogram | `tool`                              |
| `cyntr.llm.tokens.total`        | counter   | `tenant`, `provider`, `kind`        |

`status` is `ok` | `error` | `denied` | `rate_limited`.
`kind` is `input` | `output`.

## Endpoints

- `GET /api/v1/metrics` — existing JSON shape (request count, error count,
  average latency, uptime). Backward compatible.
- `GET /api/v1/metrics/prom` — Prometheus exposition of the OTel-managed
  `cyntr.*` instruments. Only available when an OTLP endpoint is configured
  (returns 404 otherwise).
- `GET /api/v1/observability/{latency,tokens,tools}` — existing
  usage-store-backed views (unchanged).

## Pointing at a backend

The OTLP/HTTP exporters are vendor-neutral. Any of these will work without
code changes:

- **Jaeger**: run with `--collector.otlp.enabled=true`, point Cyntr at
  `http://jaeger:4318`.
- **Tempo**: configure the OTLP/HTTP receiver, point at `http://tempo:4318`.
- **Grafana Cloud**: set `OTEL_EXPORTER_OTLP_ENDPOINT` to your Cloud OTLP
  endpoint and `OTEL_EXPORTER_OTLP_HEADERS=Authorization=Basic%20...`.
- **Honeycomb**: `OTEL_EXPORTER_OTLP_ENDPOINT=https://api.honeycomb.io` plus
  `OTEL_EXPORTER_OTLP_HEADERS=x-honeycomb-team=YOUR_KEY`.

## Local development with Jaeger

```yaml
# docker-compose.yml — drop in alongside Cyntr
version: "3.9"
services:
  jaeger:
    image: jaegertracing/all-in-one:latest
    environment:
      - COLLECTOR_OTLP_ENABLED=true
    ports:
      - "16686:16686"   # UI
      - "4318:4318"     # OTLP/HTTP
```

```bash
docker compose up -d jaeger
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 ./cyntr start
# Open http://localhost:16686 and select the "cyntr" service.
```

## Design notes

- **Opt-in**: instrumentation is unconditional in code, but the global
  providers are no-op when no endpoint is set. Tracer/meter calls are cheap
  enough to leave in hot paths.
- **Light surface**: only top-level user-visible operations are spanned
  (chat, tool, HTTP, IPC). Internal helpers are not — keeps trace timelines
  readable and overhead predictable.
- **No circular deps**: the observability module imports `kernel`, but
  `kernel` does not import observability. Other modules pull tracers from
  the global registry via `observability.Tracer(name)`.
