[Cyntr Docs](../README.md) > Compare > vs Hermes

# Cyntr vs Hermes

Hermes (163k stars on GitHub) is the most-loved open agent platform on the planet. If you're reading this, you're probably picking between Hermes and Cyntr for a real project. This is the honest comparison.

**Short version:** Hermes and Cyntr are aimed at different buyers. Hermes is the better choice for a solo developer or small team building a personal-assistant style agent across many messaging surfaces; Cyntr is the better choice for an engineering org that needs multi-tenant isolation, policy as code, federation, and a single static binary they can ship into a regulated environment.

## Side-by-side

| Dimension | Hermes | Cyntr |
|-----------|--------|-------|
| **License** | Open-source | Apache 2.0 |
| **Language** | TypeScript / Node | Go |
| **Deployment** | Node + npm + (usually) a process supervisor | Single static binary |
| **Footprint** | Node runtime + deps; ~100MB image | ~30MB binary; SQLite for state |
| **External infra required** | None for basics; vector DB if you want long memory | None — SQLite handles everything |
| **Memory & self-improvement** | First-class; long-term memory + skill-learning is the marquee feature | Sessions + agent long-term memory, no self-improvement |
| **Channel breadth** | Telegram, Discord, X, Farcaster, Lens, voice, more | Slack, Teams, Discord, Telegram, WhatsApp, Email, Google Chat, Webhook |
| **Skill model** | Plugins as TS packages; community plugin ecosystem | YAML skills with tool grants; marketplace + OpenClaw import |
| **Multi-tenant** | Single-instance default; multi-character | First-class tenants with isolation modes, RBAC, per-tenant quotas, OIDC |
| **Policy engine** | Per-character guardrails in code | YAML rules + OPA/Rego, allow/deny/require_approval, hot-reload |
| **Audit log** | App logs | SHA-256 hash-chained, tamper-evident |
| **Federation** | None | Cross-node delegation with peer-enforced policy |
| **Eval framework** | Community | Built-in (`cyntr eval`), JUnit output |
| **Workflow engine** | Plugin | Built-in (`agent_chat`, `tool_call`, `condition`, `parallel`, `human_input`...) |
| **Observability** | Logs + custom | OpenTelemetry traces/metrics, Prometheus endpoint |
| **Provider count** | 5+ (incl. local) | 8 (Anthropic, OpenAI, Azure, Gemini, OpenRouter, Ollama, Mock + custom) |

## Where Hermes is the right choice

**1. You want a persona across many social surfaces.** Hermes was built for character agents — a single agent identity that lives across Telegram + Discord + X + Farcaster + voice. Channel diversity is its strongest moat.

**2. Long-term memory and self-improvement matter to you.** Hermes ships a memory system that the agent can read and write against unprompted, and skills the agent can teach itself. Cyntr's memory is more conservative — explicit writes, explicit reads.

**3. You're a TypeScript shop and prefer plugin-style extensibility.** Hermes plugins are TS packages with a familiar npm-ish workflow. Cyntr extensibility is YAML tools, YAML skills, and Go modules — different shape.

**4. You want a thriving plugin ecosystem.** With 163k stars, the network effect is real. Hermes plugins outnumber Cyntr skills by a wide margin and that gap won't close soon.

**5. Your deploy target is "a Node host my friend gave me."** Hermes runs anywhere Node runs. Cyntr does too (it's a static binary), but if your habit is `npm install` you'll feel at home faster with Hermes.

## Where Cyntr is the right choice

**1. Multi-tenant from day one.** Cyntr treats tenants as a first-class boundary — isolation modes, per-tenant policy, per-tenant quotas, per-tenant audit. If you're running agents on behalf of N teams or N customers, the cost of bolting that onto Hermes after the fact is real.

**2. Policy as code.** Every tool call passes through a policy decision: YAML rules for the common case, OPA/Rego for the complex case, hot-reloadable, audited. Hermes guardrails are per-character code; if your security team wants a single policy artifact to review, Cyntr's model is much closer to what they're used to.

**3. Federation.** Two Cyntr nodes in two regions, owned by two teams, with two policies. Agent A on node 1 delegates to agent B on node 2; node 2 enforces its own policy on the inbound. No comparable feature in Hermes.

**4. Single binary, no extras.** Operations teams care about this more than developers do. A Cyntr deploy is `scp cyntr server:/usr/local/bin && systemctl start cyntr`. No Node, no Postgres, no vector DB.

**5. Regulated environments.** Hash-chained audit, PII redaction on by default, OIDC/SSO, RBAC with 4 built-in roles, data retention schedulers. The features your compliance team will ask for in week two.

**6. You want to write tools without writing code.** `tools/*.yaml` lets you wrap shell commands as tools with parameters, timeouts, and policy hooks. Hermes equivalent is a TS plugin.

## Where they're roughly equal

- **LLM provider coverage.** Both support every major commercial provider plus Ollama for local. Pick the project, not the provider list.
- **Slack/Discord/Telegram.** Both have real adapters. Cyntr's Slack story (slash commands, Block Kit, reactions for approvals, threaded replies) is more enterprise-flavored; Hermes's social-graph integrations (X, Farcaster, Lens) are more consumer-flavored.
- **Multi-agent orchestration.** Both support agent-to-agent delegation. Cyntr structures it as named crews (pipeline / parallel / sequential modes); Hermes treats it as plugin-driven coordination.

## How to decide

Pick **Hermes** if your project's center of gravity is: a character, social channels, a TS team, plugins, plugin ecosystem effects.

Pick **Cyntr** if your project's center of gravity is: an engineering org, multi-tenancy, policy review, audit, federation, single-binary ops.

If you're between the two, the deciding question is usually: *who in your organization signs off on the deploy?* If it's a developer, lean Hermes. If it's a security or platform team, lean Cyntr.

## Migration notes

There is no automated migration in either direction. Concepts that map cleanly:

- Hermes "character" ↔ Cyntr "agent (in a tenant)"
- Hermes "plugin" ↔ Cyntr "tool" (for capabilities) or "skill" (for prompt + tool bundles)
- Hermes "client" ↔ Cyntr "channel adapter"

Concepts that don't map:

- Hermes long-term memory and self-improvement — Cyntr has no direct equivalent.
- Cyntr federation — Hermes has no direct equivalent.

## Related

- [Cyntr vs Dify](vs-dify.md)
- [Cyntr vs LangChain](vs-langchain.md)
- [Concepts: Multi-tenant](../concepts/multi-tenant.md)
- [Concepts: Federation](../concepts/federation.md)
