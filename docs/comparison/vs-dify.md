[Cyntr Docs](../README.md) > Compare > vs Dify

# Cyntr vs Dify

Dify is a popular self-hosted LLM-app platform with a polished visual workflow builder. Cyntr is a self-hosted agent platform with a CLI / API / YAML config surface. Both are open-source, both are multi-tenant, both deploy on-prem. This is the honest comparison.

**Short version:** Dify wins on UX for non-engineers building LLM apps; Cyntr wins on engineering ergonomics, footprint, and the policy/audit/federation story for regulated multi-tenant environments.

## Side-by-side

| Dimension | Dify | Cyntr |
|-----------|------|-------|
| **Primary interface** | Visual workflow builder | CLI + REST API + YAML config |
| **Buyer** | Product teams, non-engineers, ops | Platform engineers, security teams |
| **Deployment footprint** | Docker Compose: API + worker + sandbox + Postgres + Redis + Weaviate (~6 services) | Single Go binary + SQLite |
| **Language** | Python | Go |
| **Image size** | ~1GB combined | ~30MB |
| **State storage** | Postgres + Redis + Weaviate | SQLite |
| **Multi-tenant model** | Workspace-based, app-level isolation | Tenant-level isolation with namespace/process modes, RBAC, quotas |
| **Policy engine** | App-level config | YAML + OPA/Rego, allow/deny/require_approval, hot-reload, audited |
| **Audit** | Logs | SHA-256 hash-chained, tamper-evident |
| **Federation** | None | Cross-node delegation with peer-enforced policy |
| **Workflow surface** | Visual canvas | JSON workflow definitions, `agent_chat` / `tool_call` / `condition` / `parallel` / `human_input` steps |
| **Tool extensibility** | Custom tools in Python | Custom tools in YAML (no code) or Go |
| **Skill marketplace** | App marketplace | Skill catalog + GitHub search + OpenClaw import |
| **Eval framework** | Manual / external | Built-in (`cyntr eval`), JUnit output |
| **Channels** | Web chat embeds, API | Slack, Teams, Discord, Telegram, WhatsApp, Email, Google Chat, Webhook |

## Where Dify is the right choice

**1. Non-engineers are building the apps.** The visual workflow builder is genuinely good. A product manager can drag together a RAG app + a knowledge base + a prompt, click publish, and ship. Cyntr makes them write JSON workflow definitions.

**2. You want app-store distribution.** Dify's app marketplace and web-chat embed cover the "I built a chatbot, now put it on my website" use case directly. Cyntr's channel surface is messaging-platform-first.

**3. You're already running Postgres + Redis + a vector DB.** The footprint cost is amortized. If you're not, it's a big tax to start.

**4. Your team writes Python.** Dify tools, custom code blocks, and extensions are Python. Cyntr's equivalent is YAML for the common case and Go for native modules — a different skill set.

**5. You need a polished knowledge-base UI.** Dify's knowledge management UI (chunking visualization, retrieval inspection) is more mature than Cyntr's dashboard knowledge page.

## Where Cyntr is the right choice

**1. Engineering-first workflow.** Cyntr's primitives are git-friendly: `cyntr.yaml`, `policy.yaml`/`policy.rego`, `tools/*.yaml`, `skills/*.yaml`, agent definitions as JSON. Code review, branch protection, CI evals on PRs — they all work because the config is just files.

**2. Single binary deploy.** `scp cyntr prod:/usr/local/bin && systemctl restart cyntr`. No compose stack, no DB schema migrations, no Redis to provision. If your platform team values operational simplicity, this is decisive.

**3. Policy as code with hot reload.** YAML for the common case, OPA/Rego for the complex case, SIGHUP to reload, every decision audited. Dify guardrails live in app config; Cyntr policy lives in a separate artifact that can be reviewed, signed, and rolled forward independently.

**4. Multi-tenant by construction.** Cyntr tenants have their own policy, their own quotas, their own audit slice, their own isolation mode. Dify treats workspaces as the boundary but doesn't take it as far. If you're running agents on behalf of N customers, this matters.

**5. Federation.** Two Cyntr nodes, two policies, agent-to-agent calls across the boundary, with the receiving node enforcing its own rules. No equivalent in Dify.

**6. Lower TCO at small scale.** A 2-vCPU box runs Cyntr + 100 tenants comfortably. The same machine struggles to run the Dify stack.

**7. Audit is built for compliance, not debugging.** Hash-chained, tamper-evident, queryable by tenant/user/action/agent/time. Dify logs are debug-grade.

## Where they're roughly equal

- **LLM provider coverage.** Both support every major commercial provider plus local via Ollama.
- **RAG.** Both can ingest documents and let agents search them. Dify's UX for managing the knowledge base is better; Cyntr's SQLite FTS5 approach is operationally simpler (no separate vector DB).
- **Streaming chat.** Both support SSE streaming responses.
- **Open-source license posture.** Both are permissive enough to deploy commercially.

## Deployment footprint, concretely

A minimal Dify deploy is six containers: `api`, `worker`, `web`, `db` (Postgres), `redis`, and `weaviate`. Compose file in their repo is ~150 lines. Memory: ~2GB resident.

A minimal Cyntr deploy is one container, one binary, one SQLite file. Memory: ~80MB resident with 10 idle agents.

This isn't a slam-dunk for Cyntr — Dify's services are mostly familiar infrastructure your team probably already runs. But "I need to convince our SRE team to give me a 6-service deploy" is sometimes the dealbreaker.

## How to decide

Pick **Dify** if your project's center of gravity is: non-engineers building LLM apps, visual workflows, web embeds, a Python team, an app-store pattern.

Pick **Cyntr** if your project's center of gravity is: engineers building agent infrastructure, multi-tenant from day one, single-binary ops, policy review, hash-chained audit, federation.

## Migration notes

Concepts that map cleanly:

- Dify "app" ↔ Cyntr "agent"
- Dify "workflow" ↔ Cyntr "workflow"
- Dify "workspace" ↔ Cyntr "tenant"
- Dify "knowledge base" ↔ Cyntr knowledge module

Concepts that don't map:

- Dify visual workflow builder — Cyntr workflows are JSON.
- Cyntr federation — Dify has no equivalent.
- Cyntr OPA/Rego — Dify has no equivalent.

## Related

- [Cyntr vs Hermes](vs-hermes.md)
- [Cyntr vs LangChain](vs-langchain.md)
- [Concepts: Multi-tenant](../concepts/multi-tenant.md)
- [Concepts: Policy](../concepts/policy.md)
