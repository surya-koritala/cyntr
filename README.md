<p align="center">
  <h1 align="center">Cyntr</h1>
  <p align="center"><strong>Self-hosted AI agent platform. Single Go binary, multi-tenant, policy-gated, federated.</strong></p>
  <p align="center">
    <a href="https://github.com/surya-koritala/cyntr/releases"><img src="https://img.shields.io/badge/release-v1.1.0-green" alt="Release"></a>
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License"></a>
    <img src="https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go" alt="Go">
    <img src="https://img.shields.io/badge/tests-37%20packages-brightgreen" alt="Tests">
  </p>
</p>

---

## Why Cyntr

Most agent frameworks are libraries — you build around them. Cyntr is a **platform** — you deploy it and it runs your agents.

- **One binary, no infrastructure.** No Postgres, no Redis, no vector DB. SQLite handles all durable state.
- **Multi-tenant from day one.** Tenants, RBAC, OIDC, per-tenant quotas, per-tenant policy.
- **Policy as code.** YAML rules for the common case, OPA/Rego for the complex case, audited on every tool call, hot-reloadable.

---

## Quick Start

```bash
git clone https://github.com/surya-koritala/cyntr.git
cd cyntr && go build -o cyntr ./cmd/cyntr
./cyntr init && ./cyntr start
```

Dashboard at **http://localhost:7700**. The wizard configures provider, channels, security, and your first agent in one flow.

Other install paths (binary, Docker): [docs/getting-started/install.md](docs/getting-started/install.md).

---

## What you can build

Cyntr is a platform; you ship verticals on top of it. Each vertical is a small pack of pre-built agents, runbook content, and a Slack-native surface — no fork of core required.

- **[Cyntr for Cloud Ops](demos/cloud-ops/)** — three pre-built agents that diagnose AWS cost spikes, troubleshoot Kubernetes workloads, and run point-in-time security audits. Slack-native, read-only by default, every tool call audit-logged. Install in 3 commands.
- **Cyntr for Customer Support** *(coming soon)* — Zendesk-aware triage agent that classifies tickets, searches the KB, and drafts a reply for a human to approve before send.
- **Cyntr for Compliance** *(coming soon)* — quarterly evidence-collection agent that pulls IAM, GitHub, and Okta state into a SOC 2 / ISO 27001 control-matrix-shaped report.

Walk through your first agent in 5 minutes: [docs/getting-started/first-agent.md](docs/getting-started/first-agent.md).

---

## How it works

```
        ┌────────────────────────────────────────────────┐
        │   CLI    Dashboard    REST API    SDKs (Py/JS)  │
        └────────────────────────┬───────────────────────┘
                                 │ HTTP
        ┌────────────────────────▼───────────────────────┐
        │                    KERNEL                       │
        │     Config · IPC Bus · Resource Manager         │
        └────────────────────────┬───────────────────────┘
                                 │ in-process messages
   ┌────────┬────────┬────────┬──┴──────┬────────┬────────┬────────┐
 Policy  Audit   Agent     Channel    Skill    Fed.    Sched.  Workflow
```

One Go binary. A small kernel boots a set of modules that talk over an in-process IPC bus. There is no message queue, no separate database, no container runtime.

Every component is a kernel module. The agent runtime calls the policy engine on every tool call; the audit logger writes a hash-chained record of every decision; the channel manager fans messages in from Slack/Teams/Discord and back out the same way. Modules boot in dependency order and shut down gracefully in reverse.

SQLite handles sessions, memory, audit, knowledge, usage, and workflow history — write-mostly, append-heavy patterns it does well. If you outgrow it, the storage layer is a Go interface; swap in Postgres without touching the rest.

Full architecture: [docs/concepts/architecture.md](docs/concepts/architecture.md).

---

## Documentation

- **[Getting started](docs/getting-started/install.md)** — install, first agent, deploy.
- **[Concepts](docs/concepts/architecture.md)** — architecture, agents, tools, skills, policy, multi-tenant, federation, observability.
- **[How-to](docs/how-to/write-a-policy.md)** — recipes for the common tasks.
- **[Reference](docs/reference/cli.md)** — CLI, REST API, config schema, env vars.
- **[Feature matrix](docs/reference/feature-matrix.md)** — every provider, tool, skill, channel, MCP server.

---

## Comparison

Cyntr vs everything else: [docs/comparison/](docs/comparison/). Short version:

- **vs [Hermes](docs/comparison/vs-hermes.md)** — Hermes wins on memory, self-improvement, and model/skill reach. Cyntr wins on multi-tenant, policy, federation, and single-binary ops.
- **vs [OpenClaw](docs/comparison/vs-openclaw.md)** — OpenClaw wins on personal-assistant breadth (5,400+ skills, 20+ channels). Cyntr wins on multi-tenant governance — and imports OpenClaw skills.
- **vs [Dify](docs/comparison/vs-dify.md)** — Dify wins on visual workflows for non-engineers. Cyntr wins on engineering ergonomics and footprint.
- **vs [LangChain](docs/comparison/vs-langchain.md)** — Category distinction: LangChain is a library, Cyntr is a platform. Often used together.

---

## Status & roadmap

**v1.1 (current).** Multi-tenant, policy (YAML + Rego), 9 channels, 8 providers, federation, evals, OpenTelemetry. Production-grade for self-hosted deploys.

**Next.** See [ROADMAP.md](ROADMAP.md). Postgres backend behind the storage interface, agent memory improvements, and a hosted control plane for fleets are on the list.

---

## Contributing & community

- [CONTRIBUTING.md](CONTRIBUTING.md) — issue triage, PR conventions, build/test commands.
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
- Issues & discussions: [GitHub Issues](https://github.com/surya-koritala/cyntr/issues)

---

## License

[Apache License 2.0](LICENSE)

<p align="center">
  <sub>Built with Go. No frameworks. No dependencies. Just code.</sub>
</p>
