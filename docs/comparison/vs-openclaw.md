[Cyntr Docs](../README.md) > Compare > vs OpenClaw

# Cyntr vs OpenClaw

OpenClaw (🦞, ~377k stars on GitHub as of June 2026) is the most-starred open
personal-AI-assistant project — "your own personal AI assistant, any OS, any
platform." If you're choosing between OpenClaw and Cyntr, this is the honest
comparison.

> Facts about OpenClaw below are taken from its public GitHub/docs (June 2026).
> If we've gotten something wrong, please file an issue — we'd rather fix it than
> be confidently wrong.

**Short version:** they're not really competitors — they're different categories
that share a skill format. OpenClaw is a **local-first, single-user personal
assistant** with a huge skill ecosystem; Cyntr is a **multi-tenant, policy-gated
platform** for an engineering org. Cyntr deliberately **imports OpenClaw skills**
(`cyntr migrate openclaw` / `cyntr skill import-openclaw`) so you don't lose that
ecosystem when you move to the platform.

## Side-by-side

| Dimension | OpenClaw | Cyntr |
|-----------|----------|-------|
| **What it is** | Personal AI assistant (single-user, local-first) | Multi-tenant agent **platform** |
| **License** | MIT | Apache 2.0 |
| **Language / runtime** | Node 22.19+/24 (npm/pnpm/bun) | Single Go binary (~30MB) |
| **External infra** | None (local) | None — SQLite for all state |
| **Memory** | Markdown files on your machine | SQLite FTS5 RAG + per-(tenant,user) memory |
| **Autonomy** | Heartbeat daemon acts on a schedule | Workflow engine + scheduler (not autonomous) |
| **Skills** | **ClawHub registry — 5,400+ skills**; SKILL.md standard | ~12 native + GitHub search + **imports OpenClaw SKILL.md** |
| **Context convention** | AGENTS.md / SOUL.md / TOOLS.md | Mirrors it: AGENTS.md / CYNTR.md / SOUL.md / TOOLS.md (hot-reloaded, path-confined per tenant) |
| **Channel breadth** | 20+ (WhatsApp, Telegram, Slack, Discord, Signal, iMessage, Teams, Matrix, WeChat, LINE, …) | 14 (Slack, Teams, Discord, Telegram, WhatsApp, Email, Google Chat, SMS, Matrix, Signal, IRC, LINE, Nostr, Webhook) |
| **Multi-tenant** | — (single user) | First-class tenants: isolation, RBAC, per-tenant quotas/policy/audit, OIDC |
| **Policy / governance** | Per-sandbox tool access | YAML rules + OPA/Rego, allow/deny/require_approval, hot-reload |
| **Audit log** | App logs | SHA-256 hash-chained, tamper-evident |
| **Sandboxing** | Docker/SSH/OpenShell for non-main sessions; main = full host | Tool allowlists + Rego policy + capability-enforcing skill sandbox |
| **Federation** | — | Cross-node delegation with peer-enforced policy |
| **Eval** | — | Built-in (`cyntr eval`), JUnit output |
| **MCP** | Full client | 8 built-in + marketplace (server token-gated) |
| **Companion/native apps** | Windows hub, macOS menu bar, iOS/Android nodes | Node-pairing protocol + reference web client (native apps deferred) |

## Where OpenClaw is the right choice

**1. You're an individual who wants the broadest off-the-shelf capability.** The
5,400+ ClawHub skills and 20+ channels are unmatched; for a personal assistant on
your own devices, nothing else has this much ready-made.

**2. Local-first and private by default.** Memory is plain Markdown on your
machine; there's no tenant/server model to operate. If "it all lives on my
laptop" is the requirement, OpenClaw fits naturally.

**3. Always-on personal autonomy.** The heartbeat daemon lets the assistant act
without being prompted — a pattern Cyntr doesn't ship.

**4. You live in Node.** OpenClaw is a Node project with a vibrant skill
community; if `npm`/`pnpm`/`bun` is home, you'll be productive immediately.

## Where Cyntr is the right choice

**1. More than one user / tenant.** Cyntr treats tenants as a hard boundary —
isolation modes, per-tenant policy, quotas, and audit. Running agents on behalf
of N teams or N customers is a category OpenClaw isn't built for.

**2. Policy as code + tamper-evident audit.** Every tool call passes a policy
decision (YAML or OPA/Rego), hot-reloadable and audited into a SHA-256
hash-chain. This is what a security team signs off on.

**3. Single static binary, no Node.** `scp cyntr server:/usr/local/bin &&
systemctl start cyntr`. No runtime to manage, no per-host npm state.

**4. Federation and regulated environments.** Cross-node delegation with
receiver-side policy, OIDC/SSO, RBAC, PII redaction, retention — the platform
features an org needs.

**5. You want OpenClaw's skills *and* governance.** You don't have to choose:
`cyntr migrate openclaw` imports your `~/.openclaw` skills and config (dry-run +
conflict-safe), so you keep the ecosystem and gain policy/tenancy/audit.

## Where they're roughly equal

- **Skill format.** Both speak SKILL.md and the AGENTS.md/SOUL.md/TOOLS.md
  context convention — Cyntr adopted it specifically for compatibility.
- **MCP.** Both are full MCP clients.
- **Mainstream channels.** Slack/Discord/Telegram/WhatsApp/Matrix/Signal exist
  on both; OpenClaw simply covers more of the long tail.

## How to decide

Pick **OpenClaw** if you're an **individual** who wants the richest personal
assistant on your own machine, with the biggest skill catalog and the most
channels.

Pick **Cyntr** if you're an **organization** that needs multi-tenancy, policy
review, audit, and single-binary ops — and use `cyntr migrate openclaw` to bring
the OpenClaw skills you already rely on.

## Migration notes

`cyntr migrate openclaw [--dry-run]` reads `~/.openclaw` and imports skills (via
the SKILL.md compat loader) and config into Cyntr. It previews changes and is
conflict-safe (it reports conflicts rather than overwriting). Concepts that map:

- OpenClaw "skill (SKILL.md)" ↔ Cyntr "skill"
- OpenClaw "workspace context files" ↔ Cyntr "context files" (AGENTS.md/…)
- OpenClaw "channel" ↔ Cyntr "channel adapter"

Concepts that don't map:

- OpenClaw's heartbeat autonomy and the breadth of ClawHub — no direct Cyntr equivalent.
- Cyntr's multi-tenancy, policy-as-code, audit hash-chain, and federation — no OpenClaw equivalent.

## Related

- [Cyntr vs Hermes](vs-hermes.md)
- [Cyntr vs Dify](vs-dify.md)
- [Cyntr vs LangChain](vs-langchain.md)
- [Concepts: Multi-tenant](../concepts/multi-tenant.md)
- [How-to: add a skill](../how-to/add-a-skill.md)
