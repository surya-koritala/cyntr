[Cyntr Docs](../README.md) > Concepts > Architecture

# Architecture

Cyntr is one Go binary. Inside it, a small `kernel` boots a set of `modules` that talk to each other over an in-process IPC bus. There is no message queue, no separate database server, no container runtime. This page explains why, and what the moving parts are.

## The shape

```
                ┌────────────────────────────────────────────────┐
                │   CLI    Dashboard    REST API    SDKs (Py/JS)  │
                └────────────────────────┬───────────────────────┘
                                         │ HTTP
                ┌────────────────────────▼───────────────────────┐
                │                    KERNEL                       │
                │  ┌──────────┐  ┌─────────┐  ┌───────────────┐  │
                │  │ Config   │  │ IPC Bus │  │ Resource Mgr  │  │
                │  └──────────┘  └────┬────┘  └───────────────┘  │
                └─────────────────────┼──────────────────────────┘
                                      │ in-process messages
   ┌────────┬────────┬────────┬───────┴────┬────────┬────────┬────────┐
   │        │        │        │            │        │        │        │
 Policy  Audit   Agent     Channel      Skill    Fed.    Sched.  Workflow
 Engine  Logger  Runtime    Mgr         Runtime  Module  Module  Engine
   │        │        │        │            │        │        │        │
   │     SQLite      │     Slack/Teams/    │   peer  │     cron       │
   │   audit.db      │     Discord/etc.    │   HTTP  │   schedule     │
   └────────┴────────┴──────────┴─────────┴─────────┴────────┴────────┘
              │
        SQLite stores
   sessions / memory / usage / knowledge / workflow
```

Every box above the SQLite line is a single Go package. Every connecting line is a method call or a typed message on the IPC bus. There is no network hop inside the binary.

## Why this shape

**Single binary**, because the operational story matters. A platform that needs Postgres + Redis + a worker queue + a scheduler dies in environments where the operator has 30 minutes and no Kubernetes cluster. Cyntr ships as one static binary; SQLite handles the durable state.

**In-process IPC**, because most "microservice" boundaries inside an agent platform are imaginary. The agent runtime calls the policy engine on every tool call. If those are over the network, you've added a round-trip per tool call for the joy of being able to scale them independently — which you don't actually need to do.

**No external DB**, because the data is small and write-heavy in a way SQLite handles well. Audit + sessions + memory + usage are all append-mostly. SQLite with WAL mode does this fine up to mid-six-figure tenants. If you genuinely outgrow it, the storage layer is a Go interface — swap in Postgres without touching the rest.

**Modules, not microservices**, because the right abstraction for "policy engine" vs "agent runtime" is a Go package boundary, not a deploy boundary. They evolve together; they ship together.

## Module lifecycle

Every module implements the `kernel.Module` interface:

```go
type Module interface {
    Name() string
    Dependencies() []string
    Init(ctx context.Context, bus *ipc.Bus, cfg *config.Config) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}
```

Boot sequence:

1. Kernel loads `cyntr.yaml`.
2. Modules are registered in dependency order. Policy and audit boot before agent runtime, because the runtime won't start without them.
3. `Init` runs for each module in topological order. This is where IPC topics are registered.
4. `Start` runs in the same order. This is where adapters connect (Slack opens its websocket, etc.).
5. On SIGTERM, `Stop` runs in reverse order. Adapters drain, in-flight requests complete, SQLite checkpoints, process exits clean. Typical drain is 3–5 seconds.

If any module fails `Init`, the process exits before serving traffic. If a module fails `Start`, dependent modules are stopped and the binary exits non-zero. There is no half-broken state.

## The IPC bus

The bus is a typed in-process message router. Modules `Subscribe(topic, handler)` at init time and `Request(topic, msg)` or `Publish(topic, event)` at runtime. Topics are strings; payloads are Go structs (no JSON inside the binary).

Backpressure: the bus has bounded channels per subscriber. A slow consumer doesn't crash producers — it gets dropped events (events) or blocking timeouts (requests). Drops are counted and exported as the `ipc.dropped` metric.

## What lives where

| Path | What it is |
|------|------------|
| `kernel/` | Bootstrap, IPC, config loader, resource manager. |
| `modules/agent/` | LLM provider abstraction, tool registry, runtime loop, session/memory/usage stores. |
| `modules/policy/` | YAML rule evaluator + optional OPA/Rego evaluator. |
| `modules/audit/` | SHA-256 hash-chained audit log over SQLite. |
| `modules/channel/` | One subpackage per messaging adapter (slack, teams, etc.). |
| `modules/proxy/` | LLM-facing reverse proxy with rate limiting and key rotation. |
| `modules/federation/` | Peer registry, outbound delegation, inbound endpoint. |
| `modules/skill/` | Catalog, marketplace, OpenClaw compat layer. |
| `modules/eval/` | Test runner for agent regression suites. |
| `modules/observability/` | OpenTelemetry exporters and Prometheus metrics. |
| `cmd/cyntr/` | The CLI. |

## What this means in practice

- One binary to deploy. Restart is fast (<2s startup).
- Adding a feature means adding a module — same shape as the existing ones.
- The IPC bus is the contract. If a feature wants to call across module boundaries, it goes on the bus. No package-level globals.
- Tests are unit-friendly because everything is interfaces and the bus has an in-memory mock.

## Related reading

- [Concepts: Agents](agents.md) — the agent runtime in detail.
- [Concepts: Policy](policy.md) — how policy decisions are made on every tool call.
- [Reference: Config](../reference/config.md) — the `cyntr.yaml` schema.
- [How-to: Add a channel](../how-to/add-a-channel.md) — writing a new module.
