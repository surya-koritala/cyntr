# Cyntr Enterprise Agent Platform — Design Specification

**Date:** 2026-03-19
**Status:** Approved
**Author:** Surya Koritala + Claude

## Overview

Cyntr is an enterprise AI agent platform built from scratch in Go. It provides a complete agent runtime with feature parity to OpenClaw, enterprise-grade security (multi-tenant RBAC, audit, compliance), and universal orchestration of external agent frameworks (OpenClaw, LangChain, CrewAI, etc.) via a policy-enforcing proxy gateway.

**Positioning:**
- **Cyntr** = enterprise agent platform (the OS for all AI agents)
- **OpenClaw** = personal agent (one of many frameworks Cyntr can orchestrate)

**Core principles:**
- Open-source (Apache 2.0), no proprietary components
- Single Go binary, minimal dependencies, no telemetry
- Secure by default, deny by default
- Real tests only — no mocks, no simulations

---

## 1. Kernel Architecture

The kernel is a thin, secure core that boots modules, manages their lifecycle, and mediates all communication.

### Kernel Components

- **Module Registry** — registers, initializes, and manages module lifecycle (start, stop, health checks)
- **IPC Bus** — in-process message bus for module-to-module communication via typed Go channels. No network overhead, but enforces clean interfaces.
- **Resource Manager** — tracks goroutines, memory, file descriptors per tenant/module. Enforces limits.
- **Config Store** — loads and distributes configuration (YAML). Modules subscribe to config changes.
- **Signal Handler** — graceful shutdown, hot config reload (SIGHUP)
- **Federation Protocol** — peer discovery, policy sync, audit replication across Cyntr instances

### Key Design Principle

Modules never talk to each other directly. All communication goes through the IPC bus, which means the kernel can log, rate-limit, and policy-check inter-module calls.

---

## 2. Modules

Eight core modules, each with a single responsibility.

### 2.1 Policy Engine

The brain of enterprise security. Every action in the system passes through here.

- **RBAC** — roles, teams, permissions mapped from enterprise IdP (SAML/OIDC)
- **Policy rules** — YAML-defined rules: per-tenant, per-team, per-agent, per-tool granularity
- **Spending controls** — budget limits per tenant/team/agent for model API calls
- **Policy evaluation** — synchronous check on every action before execution. Deny-by-default.
- **Human-in-the-loop** — escalation to approvers for sensitive actions (configurable per policy)

### 2.2 Proxy Gateway

All traffic — internal agent calls, external agent orchestration, model API requests — routes through here.

- **Inbound** — receives messages from channel adapters, authenticates, routes to correct agent
- **Outbound** — intercepts model API calls, tool invocations, external requests from agents
- **Policy enforcement point** — calls Policy Engine before forwarding any request
- **External agent proxy** — OpenClaw, LangChain, CrewAI instances route all traffic through this gateway. Framework-agnostic enforcement at the HTTP/WebSocket level.
- **Rate limiting, circuit breaking** — per-tenant, per-agent
- **Protocol support** — HTTP/HTTPS, WebSocket, gRPC
- **Intent extraction** — understands what's happening semantically (model call, tool invocation, file op, shell command) so policies can be semantic rather than URL-based

**External agent orchestration flow:**
1. Admin registers an external agent with Cyntr, specifying its type and endpoint
2. External agent is configured to route all outbound traffic through Cyntr's proxy gateway
3. Cyntr inspects every request, applies tenant policy, logs to audit, forwards or denies
4. No modification needed to the external agent — just point API base URLs at Cyntr

Example: OpenClaw configured with `ANTHROPIC_BASE_URL=http://cyntr:8080/proxy/model/anthropic` — all Claude calls flow through Cyntr's policy engine.

### 2.3 Agent Runtime

Cyntr's own agent execution engine — feature parity with OpenClaw.

- **Model adapters** — Claude, GPT, Gemini, Ollama, Azure OpenAI, Bedrock. Common interface: `ModelProvider.Chat(context, messages, tools) -> Response`
- **Tool execution** — sandboxed tool calls (shell, file, browser, HTTP). Every call goes through Policy Engine.
- **Context management** — session history, memory, skill injection per turn
- **Multi-agent** — multiple isolated agents per tenant, each with own config and permissions

### 2.4 Channel Manager

Enterprise messaging integrations.

- **Common interface** — `ChannelAdapter.Send/Receive/Authenticate`
- **Phase 1 channels** — Slack, Microsoft Teams, Google Chat, Email (SMTP/IMAP), Web UI
- **Identity mapping** — maps channel user IDs to enterprise IdP identities
- **Message routing** — routes inbound messages to correct tenant -> team -> agent

### 2.5 Skill Runtime

Secure skill execution with the new Cyntr skill format.

**Cyntr skill package structure:**
```
my-skill/
├── skill.yaml          # manifest — capabilities, dependencies, signature
├── skill.md            # instructions for the agent
├── handlers/           # executable logic (Go plugins, WASM, or scripts)
│   └── main.go
├── tests/              # required — skills without tests are rejected
│   ├── unit_test.go
│   └── live_test.go
└── SIGNATURE           # cryptographic signature
```

**skill.yaml manifest:**
```yaml
name: jira-integration
version: 1.2.0
author: cyntr-official
license: Apache-2.0

capabilities:
  network:
    - "https://*.atlassian.net"
  filesystem: none
  shell: none
  tools:
    - http_request
    - json_parse

dependencies: []

requires:
  env:
    - JIRA_API_TOKEN
  auth:
    - oidc

signing:
  registry: registry.cyntr.dev
  fingerprint: "sha256:ab12cd..."
```

**Security properties:**
- **Capability declaration** — skills declare exactly what they need. Undeclared access is denied.
- **Mandatory signing** — registry signs public skills after review. Enterprises sign private skills with their own CA.
- **Mandatory tests** — registry rejects skills without unit and live tests.
- **No transitive dependencies** — skills cannot depend on other skills. Eliminates supply chain depth attacks.

**Curated registry pipeline:** Submission -> Static Analysis -> Capability Audit -> Test Execution -> Human Review -> Sign & Publish

**OpenClaw compatibility layer:** Adapter that wraps OpenClaw `SKILL.md` format into Cyntr's format, running in a restricted compatibility sandbox (no shell, filesystem limited to /tmp, network allowlist only). Treated as untrusted by default.

### 2.6 Audit Logger

Tamper-evident, comprehensive logging for compliance.

**Audit entry structure:**
```json
{
  "id": "evt_a1b2c3d4",
  "timestamp": "2026-03-19T14:32:01.003Z",
  "instance": "cyntr-us-east-1",
  "tenant": "finance",
  "principal": {
    "user": "jane.doe@corp.com",
    "agent": "finance-analyst",
    "role": "team_lead"
  },
  "action": {
    "type": "tool_call",
    "module": "agent_runtime",
    "detail": {
      "tool": "shell_exec",
      "command": "psql -c 'SELECT ...'",
      "target": "prod-db.internal"
    }
  },
  "policy": {
    "rule": "finance-db-readonly",
    "decision": "approve",
    "decided_by": "policy_engine",
    "evaluation_ms": 2
  },
  "result": {
    "status": "success",
    "duration_ms": 340
  },
  "chain": {
    "parent_event": "evt_x9y8z7",
    "session": "sess_m4n5o6"
  },
  "signature": "sha256:..."
}
```

**What gets logged:** Auth events, policy decisions, agent actions (model calls with token count and cost, tool invocations, skill activations), admin operations (config changes, CRUD), federation events, system events (module lifecycle, health failures, resource limits).

**Tamper evidence:**
- Each entry signed with instance's private key
- Hash chain — each entry includes hash of previous entry
- Append-only storage — only Write and Query, no Update or Delete
- Federation peers cross-verify chains

**Storage:** SQLite by default (single file, no deps). Daily rotation. Configurable retention. Optional export to S3, syslog, or external SIEM.

**Compliance:** SOC 2 (comprehensive access logging + tamper evidence), GDPR (data access tracking + tenant data residency), HIPAA (access logging + process isolation for healthcare tenants), custom compliance annotations.

### 2.7 Federation

Multi-site awareness for distributed enterprise deployments.

**Topology:** All nodes are equal peers — no central controller.

**Peer discovery & trust:**
- mTLS authentication using PKI certs
- Manual peer registration or DNS-based discovery (`_cyntr._tcp.corp.com` SRV records)
- Explicit trust — admin must approve each peer

**What federates vs stays local:**

| Federates | Stays Local |
|-----------|-------------|
| Global policies | Tenant data (messages, sessions, agent state) |
| Audit metadata (queryable index) | Full audit log entries (until requested) |
| Peer health & status | Model API credentials |
| Skill registry catalog | User sessions & tokens |
| Role/permission definitions | Local policy overrides |

**Policy sync:**
- Global policies propagate to all peers within seconds
- Local policies can extend (add restrictions) but never weaken global rules
- Conflict resolution: most-restrictive-wins
- Policy versioning — peers reject out-of-order updates

**Federated audit queries:**
- On-demand, not pre-replicated (saves bandwidth, respects data locality)
- Each entry's hash chain signature verified by requesting node
- Optional continuous replication to designated compliance node

**Data residency enforcement:**
- Configured per tenant per node
- Kernel enforces — Proxy Gateway rejects requests that would move tenant data across residency boundaries
- Audit queries against residency-locked tenants return index only (who/what/when), full entry viewed on local node

### 2.8 Test Harness

Built-in testing infrastructure — no mocks, no simulations.

- **Unit tests** — individual functions and module internals against real data structures and real config
- **Integration tests** — real module combinations with real SQLite databases, real config files, real IPC bus
- **Live integration tests** — hit real external services:
  - Real LLM API calls (Claude, GPT) with test API keys
  - Real Slack/Teams instances (test workspaces)
  - Real OIDC providers (test IdP)
  - Real federated Cyntr peers (multiple instances)
  - Real OpenClaw instances for orchestration adapter testing
- **CLI commands** — `cyntr test unit`, `cyntr test integration`, `cyntr test live`
- **CI compatibility** — unit and integration tests run always; live tests on schedule or manual trigger

**Testing principles:**
- If a test can't fail when the code is broken, it's not a test
- Every policy rule gets a real-traffic integration test
- Every channel adapter gets a live test against its real platform
- Audit log integrity verified with real append-only storage
- Federation tests use real multi-instance clusters

---

## 3. Tenant Isolation (Hybrid Model)

Two modes, configurable per tenant:

### Namespace Isolation (Default)
- Agents run as goroutines in the main Cyntr process
- Isolated via Go interfaces — each tenant gets own config, policy scope, audit stream, resource quotas
- Resource Manager enforces goroutine counts, memory limits, API call budgets
- Lightweight, efficient, good for most teams
- Risk: panic in one agent could affect others (mitigated by recovery handlers)

### Process Isolation (High-Security)
- Agents run as separate OS processes spawned by kernel
- Communication via Unix domain sockets (still through IPC bus)
- Optional cgroup/namespace enforcement on Linux for CPU, memory, filesystem isolation
- Full crash isolation
- Higher overhead but complete tenant separation

### Configuration
```yaml
tenants:
  marketing:
    isolation: namespace
  finance:
    isolation: process
    cgroup:
      memory_limit: 2GB
      cpu_shares: 512
```

Escalation is one config change — flip `isolation: process` in tenant config.

---

## 4. Identity & Authentication

### Flow
1. User authenticates via enterprise IdP (SAML/OIDC). Cyntr never stores passwords.
2. Identity Mapper resolves IdP user to Cyntr principal — enriched with tenant, roles, teams, agent access.
3. Channel identity binding — Slack/Teams/email user IDs bound to same Cyntr principal.
4. Session Manager issues short-lived JWTs for web UI/API. Federated peers use mTLS. Service accounts use scoped API keys.

### RBAC Model
```yaml
roles:
  admin:
    - manage_tenants
    - manage_policies
    - manage_agents
    - view_all_audit
  team_lead:
    - manage_team_agents
    - manage_team_skills
    - approve_actions
    - view_team_audit
  user:
    - interact_with_agents
    - view_own_audit
  auditor:
    - view_all_audit
    - export_audit
```

Roles assigned per tenant. Users can hold different roles in different tenants. Roles map from IdP groups (e.g., AD group `Finance-Admins` -> `admin` role in `finance` tenant).

### Agent Identity
Agents are principals too — each has an identity, API credentials, policy scope, and audit trail. Audit answers both "what did user X do?" and "what did agent Y do on behalf of user X?"

---

## 5. CLI & Web UI

### CLI (`cyntr` command)

```bash
# Instance management
cyntr start / stop / status / doctor

# Tenant management
cyntr tenant create <name> --isolation <mode>
cyntr tenant list / config

# Agent management
cyntr agent create / list / logs / attach

# External agent orchestration
cyntr proxy register / list / traffic

# Policy
cyntr policy apply / test / list

# Skills
cyntr skill install / list / verify

# Audit
cyntr audit query / export / verify

# Federation
cyntr federation join / peers / sync-status

# Testing
cyntr test unit / integration / live
```

### Web UI

Served by the kernel (default `:7700`). Static assets compiled into the binary. Authenticates via enterprise IdP (SAML/OIDC).

**Pages:** Dashboard (health, approvals, recent audit), Agents (list, config, logs, chat), Tenants (manage, isolation, resources), Policy (visual editor, test, enforcement stats), Skills (registry, install, capabilities), Audit (search, filter, export, chain verification), Federation (peer status, sync health, cross-site queries), Proxy (external agent traffic, request log, policy hits).

---

## 6. Project Structure

```
cyntr/
├── cmd/cyntr/main.go                    # binary entrypoint
├── kernel/
│   ├── kernel.go                        # init, module registration, lifecycle
│   ├── ipc/bus.go, types.go             # in-process message bus
│   ├── config/store.go, schema.go       # YAML config, validation
│   ├── resource/manager.go              # per-tenant resource tracking
│   └── module.go                        # Module interface
├── modules/
│   ├── policy/engine.go, rules.go, approval.go
│   ├── proxy/gateway.go, router.go, enforcement.go
│   ├── agent/runtime.go, context.go, providers/*.go
│   ├── channel/manager.go, adapters/*.go
│   ├── skill/runtime.go, registry.go, format.go, compat/openclaw.go
│   ├── audit/logger.go, query.go, verify.go
│   └── federation/peer.go, sync.go, query.go
├── tenant/tenant.go, namespace.go, process.go
├── auth/oidc.go, saml.go, identity.go, session.go
├── web/server.go, api/v1/, static/
├── tests/unit/, integration/, live/
├── docs/superpowers/specs/
├── go.mod, go.sum
├── LICENSE                              # Apache 2.0
└── cyntr.yaml                           # default config example
```

**Conventions:**
- Each module implements `kernel.Module` interface: `Name()`, `Init(kernel)`, `Start()`, `Stop()`, `Health()`
- Modules receive `kernel.Context` on init — access to IPC bus, config store, resource manager
- All inter-module calls go through IPC bus, never direct imports between `modules/*` packages
- `tenant/` and `auth/` are shared packages used by kernel and modules
- Unit tests alongside code (`*_test.go`), cross-module tests in `tests/`
