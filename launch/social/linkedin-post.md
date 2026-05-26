# LinkedIn launch post

> Post Tuesday around 10:30 AM Pacific — after the HN traction is
> visible. Different tone from Twitter: business-impact framing, no
> emojis, slightly longer paragraphs, no thread.

---

We're open-sourcing Cyntr today — an AI agent platform built for teams who want to put agents into production without rebuilding the operational layer themselves. Single Go binary, OPA Rego policy-as-code, federation across independent nodes, SHA-256 audit hash chains, multi-tenant isolation, OIDC/SSO. Apache 2.0.

The pattern we kept watching: someone ships an agent prototype, gets approval to roll it out, and then spends the next quarter bolting on the boring stuff — tenants, audit, policy, RBAC, channel adapters, eval. By the time it's in production they've built a platform; they just hadn't planned to. So we built it as a platform from day one. You deploy a single binary; you get tenants, policy, audit, dashboard, scheduler, workflows, eight LLM providers, and nine messaging channels out of the box.

Two pieces we think are genuinely new for this category. First, federation: you can run independent Cyntr instances — each with its own tenants, agents, and LLM keys — and have agents delegate work across the boundary, with the receiving node's policy always deciding. This is the right shape for cross-team deployments, data-residency requirements, and tiered-trust architectures where the high-trust node owns the credentials. Second, policy-as-code with OPA Rego: security teams can review what agents are allowed to do in the same PR-and-CI workflow they already use for Kubernetes admission control. No new tool to learn, no UI checkbox to misclick.

Honest framing: this is v1.1. The Curator (agent-builds-agent) and several vertical packs are on the roadmap; the skill marketplace catalog is small but growing. The federation demo runs in two minutes (`go test ./demos/federation/ -v`) and is the most concrete proof of the differentiation; that's where we'd start.

If your team is past the prototype stage on AI agents and stuck on the production wall — audit, policy, multi-tenant, channel integrations — we'd love to hear what's in your way. Repo and federation demo in the comments. We're in Discord all week.

#OpenSource #AIAgents #DevOps #PlatformEngineering #Go
