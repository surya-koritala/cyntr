# Cyntr vs Hermes Agent vs Dify vs LangChain

> A neutral comparison page for cyntr.dev/compare. Numbers and feature
> claims are as of 2026-05-26 from each project's README / docs. If
> you spot something we've gotten wrong, please file an issue — we'd
> rather correct the page than be slightly wrong forever.

LangChain is included as a framework reference, not a like-for-like
competitor. The other three are platforms.

## Reading guide

A "Yes" / "Built-in" cell means the feature ships in the default
install and is supported by the project as a first-class capability.
"Plugin" means a community plugin exists but isn't core. "—" means
the feature isn't applicable to that project's shape (e.g. asking
LangChain whether it has multi-tenant isolation is a category error;
LangChain is a library, you'd build that yourself).

We've tried to be fair. Where a competitor wins, the table says so.

## The table

| Dimension                          | Cyntr v1.1                                    | Hermes Agent                                | Dify                                           | LangChain (framework)                  |
|------------------------------------|-----------------------------------------------|---------------------------------------------|------------------------------------------------|----------------------------------------|
| **License**                        | Apache 2.0                                    | Apache 2.0                                  | Dify Open Source License (Apache-ish, with conditions on hosted resale) | MIT                                    |
| **Runtime / footprint**            | Single Go binary, ~40 MB, SQLite-only         | Python + Node, multi-service docker-compose | Python + Node, multi-service docker-compose    | Python library, your runtime           |
| **External deps**                  | None (SQLite embedded)                        | Postgres, Redis, OpenSearch (typical)       | Postgres, Redis, Weaviate/Qdrant (typical)     | Whatever your app uses                 |
| **Multi-tenant isolation**         | Yes — kernel primitive, per-tenant policy, audit, rate-limit, provider config | Limited — workspace concept, not hard isolation             | Yes — workspaces with RBAC                     | —                                      |
| **Policy / audit**                 | OPA Rego + YAML rules; SHA-256 audit hash chain | Per-plugin permissions, no policy-as-code; audit available     | Workspace-level permissions; audit log         | —                                      |
| **Channels (Slack/Teams/Discord/etc.)** | 9 built-in adapters                     | Strong plugin ecosystem (community-driven)  | Slack + a few others built-in; rest via webhook | —                                      |
| **RAG / knowledge base**           | Built-in — SQLite FTS5, no external vector DB | Plugin-based                                | Built-in — Weaviate/Qdrant backed              | Plugin (Chroma/Pinecone/etc.)          |
| **Federation across nodes**        | Yes — cross-node delegation with receiver-side policy enforcement | No                                          | No                                             | —                                      |
| **Eval framework**                 | Built-in — `cyntr eval`, JUnit output, CI exit codes | No                                          | Some — workflow-level testing                  | langchain-eval (separate package)      |
| **Sandboxing**                     | Tool-level allowlists + Rego policy + approval gates | Plugin-level permissions                    | Workflow node permissions                      | You write it                           |
| **Skill / plugin marketplace**     | Built-in skill catalog + GitHub search + OpenClaw import | Yes — large plugin ecosystem (the leader here) | Yes — workflow templates and tools             | —                                      |
| **Visual workflow builder**        | Workflow engine via JSON / API; no drag-and-drop yet | Some — limited workflow UI                  | Yes — best-in-class drag-and-drop (the leader here) | —                                      |
| **Hosted offering**                | try.cyntr.dev — free sandbox; paid managed planned | No first-party hosted                       | dify.ai — managed cloud, paid tiers            | LangSmith (observability/managed eval) |
| **LLM providers supported**        | 8 (Anthropic, OpenAI, Azure OpenAI, Gemini, OpenRouter, Ollama, Mock, …) | 20+ via plugins                             | 15+ built-in                                   | Effectively unlimited (it's a library) |
| **MCP server support**             | 8 built-in + marketplace                      | Plugin                                      | Not yet                                        | Community                              |
| **GitHub stars (2026-05-26)**      | small (early launch — that's why this page exists) | ~163k                                       | ~50k                                           | ~95k (langchain-ai/langchain)          |
| **GitHub launch year**             | 2026                                          | 2023                                        | 2023                                           | 2022                                   |

## Where each project wins

We don't think this is a winner-take-all category, and pretending
otherwise would be dishonest. Here's where each project is the right
pick:

- **Hermes Agent** wins for the consumer / personal-assistant
  pattern, especially anywhere a large existing plugin ecosystem
  matters more than enterprise primitives. If you're an individual
  user or a small team and you want the broadest set of off-the-shelf
  capabilities, this is the choice.
- **Dify** wins for the no-code-first deployment: a non-engineer
  building agent workflows by dragging boxes around. The visual
  builder is years ahead of anything else in OSS, and the recent
  enterprise-feature work (RBAC, multi-tenancy) is closing the gap
  on the operational side. If your decision-maker is a product
  manager who wants to ship without writing code, pick Dify.
- **LangChain** wins for prototyping and for "I'm a developer
  writing a Python program that happens to use an LLM." It's a
  library, the surface area is huge, and you have full control. If
  you're at the prototype stage, start here.
- **Cyntr** wins when your decision-driver is the security or
  platform-engineering team: OPA Rego policy-as-code, hard
  multi-tenant in a single binary, federation across nodes, audit
  hash chains, eval-as-CI. If your blocker to shipping an agent
  product is "what will the CISO sign off on," we built for you.

## What we'd want to change about Cyntr (the honest list)

- **Visual workflow builder.** We don't have one. Dify is the leader
  here and we won't catch up this year.
- **Skill catalog breadth.** The plumbing is there; the catalog is
  ~12 skills. Hermes is far ahead on ecosystem.
- **Streaming polish.** Provider-level token streaming for Slack and
  the dashboard isn't great yet.
- **HA / clustering.** Cyntr is single-node per cluster; federation
  is the cross-node story but not the HA story. If you need
  active-active failover, neither Hermes nor Dify ships that
  either, but we want to call out that none of us do.

## How this page should be used

Pick the project that matches your *decision-driver*, not the one
that wins the most cells. The cells are facts; the right answer
depends on whose problem you're solving.

If you'd like to discuss a specific use-case, we're at
discord.gg/cyntr (link in repo) and we'll be honest if a competitor
is a better fit.

---

*Last reviewed: 2026-05-26. Send corrections to issues@cyntr.dev or
file an issue on the repo.*
