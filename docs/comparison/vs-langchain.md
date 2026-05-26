[Cyntr Docs](../README.md) > Compare > vs LangChain

# Cyntr vs LangChain

LangChain is a **library** for building LLM applications. Cyntr is a **platform** you deploy. The comparison is mostly a category distinction.

**Short version:** if you're writing code that calls LLMs, use LangChain (or LangGraph). If you're running agents on behalf of users in a multi-tenant environment, use Cyntr. Many real deployments use both — LangChain in the application code, Cyntr as the surrounding infrastructure.

## The category distinction

| Dimension | LangChain | Cyntr |
|-----------|-----------|-------|
| **What it is** | Library imported into your app | Server you deploy alongside your apps |
| **You write** | Application code in Python/JS that orchestrates LLMs | Agent definitions, tools (YAML or Go), policies |
| **Lifecycle** | Lives inside your process | Standalone process |
| **Multi-tenant** | Up to you to build | First-class |
| **Policy** | Up to you to build | Built in |
| **Audit** | Up to you to build | Built in |
| **Eval** | LangSmith (paid SaaS) or BYO | Built in (`cyntr eval`) |
| **Channels** | Up to you to wire | 9 adapters built in |

You don't pick LangChain *or* Cyntr the way you pick Hermes *or* Cyntr. You pick LangChain because you're writing application logic; you pick Cyntr because you want a platform that hosts agents.

## When to use LangChain (or LangGraph)

- You're building bespoke LLM logic inside a larger application.
- You want fine-grained control over chain composition, retrieval, prompt engineering primitives.
- You don't need multi-tenant isolation, policy as code, audit, or channel adapters — your app already handles those at a different layer.
- You're a Python shop and you want a library, not a server.

## When to use Cyntr

- You're standing up an agent platform that serves multiple teams or customers.
- You need tenants, policy, audit, RBAC, OIDC, federation, quotas — and you don't want to build them.
- Your agents need to live in Slack/Teams/Discord/etc.
- You want a single binary deploy, not a Python service to maintain.

## Using them together

The common pattern is: write your custom RAG / chain logic with LangChain inside a worker, and front it with a Cyntr "tool" that calls into your worker. Cyntr handles multi-tenancy, policy, audit, channels; LangChain handles the application logic.

A YAML tool that calls your worker:

```yaml
name: my_custom_pipeline
description: Run the proprietary RAG pipeline.
parameters:
  query: { type: string, required: true }
command: "curl -s http://my-worker:8000/run -d '{\"q\": \"{{.query}}\"}'"
timeout: 30s
```

The Cyntr agent calls this tool; the worker is pure LangChain.

## Related

- [Cyntr vs Hermes](vs-hermes.md)
- [Cyntr vs Dify](vs-dify.md)
- [Concepts: Architecture](../concepts/architecture.md)
