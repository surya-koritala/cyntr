# Cyntr Documentation

Self-hosted AI agent platform. Single Go binary, multi-tenant, policy-gated, federated.

## Start here

New to Cyntr? Read these in order and you'll have an agent running in under 30 minutes.

1. [Install](getting-started/install.md) — binary, Docker, or source. 5 commands.
2. [Your first agent](getting-started/first-agent.md) — chat via CLI, API, Slack.
3. [Production checklist](getting-started/deploy.md) — what to harden before shipping.

## Concepts

Read these when you want to understand *why* something works the way it does.

- [Architecture](concepts/architecture.md) — kernel, IPC bus, modules.
- [Agents](concepts/agents.md) — model + prompt + tools + skills.
- [Tools](concepts/tools.md) — built-ins, YAML tools, MCP servers.
- [Skills](concepts/skills.md) — bundled prompt + tool grants, loaded on demand.
- [Policy](concepts/policy.md) — YAML rules and Rego, with reload semantics.
- [Multi-tenant](concepts/multi-tenant.md) — tenants, RBAC, OIDC, quotas.
- [Federation](concepts/federation.md) — cross-node agent delegation.
- [Observability](concepts/observability.md) — OpenTelemetry traces and metrics.

## How-to

Recipe-style guides. Each one starts with what you'll have when you're done.

- [Add a tool](how-to/add-a-tool.md)
- [Add a skill](how-to/add-a-skill.md)
- [Write a policy](how-to/write-a-policy.md)
- [Add a channel](how-to/add-a-channel.md)
- [Run evals in CI](how-to/run-evals.md)
- [Configure LLM providers](how-to/configure-llm-providers.md)
- [Deploy to Fly.io](how-to/deploy-to-fly.md)

## Compare

Honest side-by-sides. Use these to figure out if Cyntr is the right fit.

- [Cyntr vs Hermes](comparison/vs-hermes.md)
- [Cyntr vs Dify](comparison/vs-dify.md)
- [Cyntr vs LangChain](comparison/vs-langchain.md)

## Reference

Exhaustive, machine-readable surface area.

- [CLI](reference/cli.md) — every `cyntr` subcommand.
- [REST API](reference/api.md) — 80+ endpoints, envelope, auth.
- [Config](reference/config.md) — full `cyntr.yaml` schema.
- [Environment variables](reference/env-vars.md) — every variable the binary reads.
- [Feature matrix](reference/feature-matrix.md) — providers, tools, skills, channels, MCP servers.

## Project links

- [Main README](../README.md)
- [Contributing](../CONTRIBUTING.md)
- [Roadmap](../ROADMAP.md)
- [License (Apache 2.0)](../LICENSE)
