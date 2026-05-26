# Show HN post

## Title (78 chars, em-dash, HN-compliant)

```
Show HN: Cyntr – Open-source AI agent platform with Rego and federation
```

## URL field

```
https://github.com/surya-koritala/cyntr
```

## Body (paste into the text field — keep paragraphs short, no markdown headers, HN strips most formatting)

Hi HN — we've been building Cyntr for the last several months and v1.1 is now in a state where we feel okay showing it. It's a single Go binary that runs AI agents (Claude, GPT, Gemini, Ollama) with the boring parts already done: multi-tenant isolation, OPA Rego policy-as-code, SHA-256 audit hash chains, OIDC/SSO, and a 17-page dashboard. SQLite is the only data store; there's no Redis, no Kafka, no Postgres requirement.

Two things make Cyntr different from the usual "agent framework" pile. First, the policy engine takes Rego files (or simpler YAML rules) and evaluates every tool call against them, so you can say "shell_exec is allowed for the cloud-ops agent in tenant X but require_approval everywhere else" without writing Go. Second, federation: you can run independent Cyntr nodes — each with its own tenants, policies, and LLM keys — and have agents delegate work across the boundary. The receiving node's policy always decides. There's a runnable two-node demo in `demos/federation/` that anyone can try in ~2 minutes (`go test ./demos/federation/ -v` or `./demos/federation/run.sh`).

We think the sweet spot is the team that wants a self-hosted alternative to building on LangChain plus glue code — cloud-ops automations, internal Slack bots that touch infra, customer-support agents that need to be auditable. The federation demo shows the cross-org / cross-region story we care most about; there's also an evaluation framework (`evals/`) we use as CI so model upgrades don't quietly regress.

Honest caveats: this is v1.1, not v3.0. The skill marketplace is built-in but the catalog is small; the hosted offering is a free-tier sandbox; the Curator (agent-builds-agent) is on the roadmap, not shipped. Apache 2.0. We'd love feedback on the policy model, the federation trust boundary, anything you'd want before deploying this at your own company.

Repo: https://github.com/surya-koritala/cyntr
Federation demo: https://github.com/surya-koritala/cyntr/tree/main/demos/federation
Docs: https://cyntr.dev/docs

---

## Pre-drafted top-comment replies

These are answers we expect to need on launch day. Paste-ready, written in our voice.

### Reply A — "How is this different from Hermes Agent / Dify?"

> Fair question — we've been watching both for a while. Hermes is the consumer-leaning one (163k stars, brilliant chat-first UX, huge plugin ecosystem). Dify has a great visual workflow builder and a long head start there; we won't catch up on drag-and-drop UX this year. Where we think Cyntr is genuinely different: (1) policy-as-code with OPA Rego, not just YAML allow-lists, so security teams can review the rules the same way they review Kubernetes admission policies; (2) federation across independent nodes — we don't know of another OSS agent platform shipping it; (3) hard multi-tenant isolation in a single binary (each tenant gets its own namespace and policy scope), not "you run one copy per team." If you're picking a platform today and your decision driver is "best visual workflow builder", Dify is the safer bet. If it's "I need to deploy this in a regulated environment and prove what every agent did", we'd like a shot.

### Reply B — "Why Go? Most of the agent ecosystem is Python."

> Three reasons, in order of how much they mattered. One: single static binary, no venv, no Python version drift on customer machines — that matters a lot for self-hosted enterprise. Two: the boring parts of an agent platform (HTTP server, SQLite, audit log, scheduler, policy evaluator, federation transport) are way easier in Go than in Python, and the LLM client code is the easy part either way — it's just HTTP. Three: we wanted to embed OPA cleanly, and the Go OPA library is the reference implementation. The cost is real though: agent authors write Python more naturally, so we ship a Python SDK and a YAML "tool" format that lets you wrap a shell command without writing any Go. The skill system itself is data, not code — system prompt + tool permissions + config in JSON — so adding a skill doesn't mean writing Go either.

### Reply C — "How is this different from LangChain?"

> LangChain is a library — you import it, you write a Python program, you deploy that program somewhere, and you build all the operations stuff (auth, audit, multi-tenant, dashboard, approvals, channels) yourself. Cyntr is the deploy target — you run `./cyntr start`, point a Slack bot or REST client at it, and you get tenants, audit, RBAC, OIDC, policy, scheduler, workflows, channel adapters (Slack/Teams/Discord/Telegram/email), and a dashboard out of the box. We're not trying to replace LangChain for the "I'm prototyping an LLM idea on my laptop" workflow — LangChain is great at that and we wouldn't compete. Cyntr is for the moment after, when someone says "okay, now actually run this for the company." If your shop has standardised on LangChain for the orchestration logic, you can still call into Cyntr via the REST API and use it as the audit / policy / channel layer.
