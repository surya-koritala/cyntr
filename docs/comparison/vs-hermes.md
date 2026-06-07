[Cyntr Docs](../README.md) > Compare > vs Hermes

# Cyntr vs Hermes

Hermes Agent (by Nous Research, ~185k stars on GitHub as of June 2026) is one of the most-loved open agent platforms on the planet. If you're reading this, you're probably picking between Hermes and Cyntr for a real project. This is the honest comparison.

> Facts about Hermes below are taken from its public GitHub/docs (June 2026). If we've gotten something wrong, please file an issue — we'd rather fix it than be confidently wrong.

**Short version:** Hermes and Cyntr are aimed at different buyers. Hermes is the better choice for a solo developer or small team building a personal-assistant style agent across many messaging surfaces; Cyntr is the better choice for an engineering org that needs multi-tenant isolation, policy as code, federation, and a single static binary they can ship into a regulated environment.

## Side-by-side

| Dimension | Hermes | Cyntr |
|-----------|--------|-------|
| **License** | MIT | Apache 2.0 |
| **Language** | Python (+ some TypeScript) | Go |
| **Deployment** | Python (uv) + a gateway process; Docker, or serverless (Modal/Daytona) | Single static binary |
| **Footprint** | Python env + deps | ~30MB binary; SQLite for state |
| **External infra required** | None for basics; uses SQLite (FTS5) for session history | None — SQLite handles everything |
| **Memory & self-improvement** | **The marquee feature** — a closed learning loop: agent-curated memory (MEMORY.md/USER.md), FTS5 history + LLM summary, autonomous skill creation + refinement | Sessions + per-(tenant,user) memory; opt-in trajectory capture; curator can judge/improve skills — but no autonomous self-rewrite loop |
| **Channel breadth** | Telegram, Discord, Slack, WhatsApp, Signal, Email (single gateway process) | Slack, Teams, Discord, Telegram, WhatsApp, Email, Google Chat, SMS, Matrix, Signal, IRC, LINE, Nostr, Webhook |
| **Skill model** | Python skills/plugins in `~/.hermes/skills`; agentskills.io standard; imports OpenClaw | YAML skills with tool grants; marketplace + OpenClaw import |
| **Multi-tenant** | Single-user (multi-tenant messaging via gateway pairing) | First-class tenants with isolation modes, RBAC, per-tenant quotas, OIDC |
| **Policy engine** | Command allowlist patterns + DM pairing | YAML rules + OPA/Rego, allow/deny/require_approval, hot-reload |
| **Audit log** | App logs | SHA-256 hash-chained, tamper-evident |
| **Federation** | None | Cross-node delegation with peer-enforced policy |
| **Eval framework** | Batch trajectory generation/compression (for training) | Built-in (`cyntr eval`), JUnit output |
| **Workflow engine** | Cron scheduler + subagents | Built-in (`agent_chat`, `tool_call`, `condition`, `parallel`, `human_input`...) |
| **Observability** | Logs + `/insights`/`/usage` introspection | OpenTelemetry traces/metrics, Prometheus endpoint |
| **Provider count** | 300+ via Nous Portal / 200+ via OpenRouter, plus Anthropic/OpenAI/NIM/etc. | 8 (Anthropic, OpenAI, Azure, Gemini, OpenRouter, Ollama, Mock + custom) |

## Where Hermes is the right choice

**1. You want an agent that gets smarter over time.** Hermes's marquee feature is a genuine closed learning loop — it curates its own memory, creates skills from experience, and refines them in use. If "compounds the longer it runs" is your goal, this is the strongest pick.

**2. You want the widest model and skill reach.** 300+ models via Nous Portal / 200+ via OpenRouter, dynamic `/model` switching, plus a large community skill ecosystem (and it imports OpenClaw skills). Cyntr's provider list (8) and native catalog are deliberately narrower.

**3. You're a Python shop and prefer plugin-style extensibility.** Hermes skills/plugins are Python in `~/.hermes/skills` with a familiar workflow. Cyntr extensibility is YAML tools, YAML skills, and Go modules — a different shape.

**4. You want a thriving ecosystem and community.** At ~185k stars the network effect is real; Hermes's skill/plugin breadth outnumbers Cyntr's native catalog by a wide margin and that gap won't close soon.

**5. Your deploy target is a personal box or serverless.** Hermes runs on a $5 VPS, Termux, Docker, or hibernating serverless (Modal/Daytona). Cyntr runs anywhere too (static binary), but Hermes is purpose-built for the single-user, always-on personal-agent shape.

## Where Cyntr is the right choice

**1. Multi-tenant from day one.** Cyntr treats tenants as a first-class boundary — isolation modes, per-tenant policy, per-tenant quotas, per-tenant audit. If you're running agents on behalf of N teams or N customers, the cost of bolting that onto Hermes after the fact is real.

**2. Policy as code.** Every tool call passes through a policy decision: YAML rules for the common case, OPA/Rego for the complex case, hot-reloadable, audited. Hermes guardrails are per-character code; if your security team wants a single policy artifact to review, Cyntr's model is much closer to what they're used to.

**3. Federation.** Two Cyntr nodes in two regions, owned by two teams, with two policies. Agent A on node 1 delegates to agent B on node 2; node 2 enforces its own policy on the inbound. No comparable feature in Hermes.

**4. Single binary, no extras.** Operations teams care about this more than developers do. A Cyntr deploy is `scp cyntr server:/usr/local/bin && systemctl start cyntr`. No Node, no Postgres, no vector DB.

**5. Regulated environments.** Hash-chained audit, PII redaction on by default, OIDC/SSO, RBAC with 4 built-in roles, data retention schedulers. The features your compliance team will ask for in week two.

**6. You want to write tools without writing code.** `tools/*.yaml` lets you wrap shell commands as tools with parameters, timeouts, and policy hooks. Hermes equivalent is a TS plugin.

## Where they're roughly equal

- **LLM provider coverage.** Both reach every major commercial provider plus local (Ollama). Hermes reaches far more models via aggregators; Cyntr ships a curated set. For "can it talk to model X," assume yes for both.
- **Slack/Discord/Telegram/WhatsApp.** Both have real adapters for the mainstream messengers. Cyntr's Slack story (slash commands, Block Kit, reactions for approvals, threaded replies) is more enterprise-flavored; Hermes leans personal-assistant.
- **Multi-agent orchestration.** Both support agent-to-agent delegation. Cyntr structures it as named crews (pipeline / parallel / sequential modes); Hermes treats it as plugin-driven coordination.

## How to decide

Pick **Hermes** if your project's center of gravity is: a self-improving personal agent, a Python team, the widest model/skill reach, ecosystem effects.

Pick **Cyntr** if your project's center of gravity is: an engineering org, multi-tenancy, policy review, audit, federation, single-binary ops.

If you're between the two, the deciding question is usually: *who in your organization signs off on the deploy?* If it's a developer, lean Hermes. If it's a security or platform team, lean Cyntr.

## Migration notes

There is no automated migration in either direction. Concepts that map cleanly:

- Hermes "agent/personality" ↔ Cyntr "agent (in a tenant)" + switchable personalities
- Hermes "skill/plugin" ↔ Cyntr "tool" (for capabilities) or "skill" (for prompt + tool bundles)
- Hermes "gateway adapter" ↔ Cyntr "channel adapter"
- Both can import OpenClaw SKILL.md skills (`hermes claw migrate` ↔ `cyntr migrate openclaw`)

Concepts that don't map:

- Hermes's autonomous learning loop (self-created/self-refining skills) — Cyntr has opt-in trajectory capture and a curator, but no autonomous self-rewrite.
- Cyntr federation, hard multi-tenancy, and policy-as-code — Hermes has no direct equivalent.

## Related

- [Cyntr vs OpenClaw](vs-openclaw.md)
- [Cyntr vs Dify](vs-dify.md)
- [Cyntr vs LangChain](vs-langchain.md)
- [Concepts: Multi-tenant](../concepts/multi-tenant.md)
- [Concepts: Federation](../concepts/federation.md)
