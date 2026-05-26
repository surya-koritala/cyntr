# r/devops launch post

> Post Wednesday around 10:30 AM Pacific. r/devops won't tolerate
> "AI agent platform" framing alone — lean into the Cloud Ops vertical
> and the policy/audit story. Frame as "your AI agent for cloud-ops
> with the audit trail your security team will accept."

---

## Title

```
[Release] Cyntr v1.1 — self-hosted AI agent for cloud-ops with OPA
Rego policy and audit hash chains (open source, Apache 2.0)
```

(151 chars — under Reddit's title cap.)

## Body

```
Hi r/devops — we're shipping the first usable version of Cyntr today.
It's an open-source platform for running AI agents that touch real
infrastructure — kubectl, AWS/Azure/GCP CLI, internal runbooks — with
the controls your security team actually wants:

– OPA Rego policy-as-code for every tool call. Same evaluator your
  Kubernetes admission controller uses, applied to "can this agent
  run `aws ec2 terminate-instances`."
– SHA-256 audit hash chains. Every action the agent took, who
  approved it, when, against which policy version. Tamper-evident.
– Multi-tenant in a single binary. The cloud-ops bot for the
  platform team and the cost-analyst bot for finance run in the
  same process with isolated agent namespaces, policy scopes, and
  audit slices.
– Blocking approval mode for destructive operations. The agent
  posts to a Slack approval channel; a human reacts with ✅; the
  agent continues. 5-minute timeout, no auto-approve.
– Read-only by default. The cloud-ops agent ships with a system
  prompt and policy that allow `kubectl get/describe/logs` and
  block writes. Loosening it is a deliberate edit, not an accident.

The use-case we built for first: the on-call engineer at 2 AM who
needs to know what's broken before they're awake enough to type
`kubectl`. Slack message → agent runs read-only diagnostics →
proposes a remediation → human reacts to approve → agent executes
the remediation → everything logged with a hash chain.

Stack: single Go binary, ~40 MB. SQLite for state. No external deps.
Runs anywhere `kubectl` runs. 8 LLM providers including Ollama if
you don't want cloud calls.

Quick start:

  git clone https://github.com/surya-koritala/cyntr.git
  cd cyntr && go build -o cyntr ./cmd/cyntr
  ./cyntr init                 # 5-step wizard
  ./cyntr start
  # http://localhost:7700

The federation demo (`demos/federation/`) is worth two minutes if
you're in a multi-team org — independent Cyntr nodes per team,
agents delegating across the boundary, each node's policy gating
inbound delegation. We use it for the "infra team owns the cloud
creds; product teams delegate to it under approval" pattern.

Honest framing: this is v1.1. The Curator (agent-builds-agent), more
vertical packs (SRE-incident-response, cost-anomaly-bot), and a
hosted control plane are on the roadmap. The federation story, the
policy story, and the audit story all work today.

Repo: https://github.com/surya-koritala/cyntr
Federation demo: https://github.com/surya-koritala/cyntr/tree/main/demos/federation
Docs: https://cyntr.dev/docs

Would love feedback from anyone running on-call rotations who's
thought about agent-assisted ops and bounced off the audit / policy
question. That's exactly who we built for.
```

## Comment to drop in 60 seconds after posting

> One thing I should call out before the inevitable question: there's
> no "the agent runs `terraform apply` for you" feature here, and we
> don't plan to ship one without significant friction. Read-only by
> default, write operations gated by blocking human approval, every
> action audited. We think autonomous-write-to-prod agents are a
> failure mode waiting to happen and we'd rather lose the demo
> spectacle than ship them.

## Reply templates

### "OPA Rego vs the YAML rules — which should I use?"
> Start with YAML. The rule format covers the common cases (allow/deny/
> require-approval per tenant + agent + tool), and it's a 3-line
> change to revoke a permission. Move to Rego when (a) you have a
> security team that already writes Rego for Gatekeeper / Kyverno and
> wants a single review surface, or (b) your policy needs are
> stateful — "deny if this tenant has already spent more than $X this
> month" type rules. Same `CheckRequest` interface, you can have both
> backends loaded at once.

### "Can the agent actually run kubectl, or is this another
> get-permission-and-stop-there demo?"
> It actually runs kubectl. Read-only verbs (get, describe, logs, top)
> by default; write verbs (apply, delete, exec) gated by approval
> policy. The agent's tool definitions are in `modules/tool/kubectl/`
> and the demo in `demos/federation/` doesn't use it (federation
> demo uses the mock provider) but the cloud-ops agent that ships
> with `cyntr init` does. Real kubectl, real cluster, real audit.

### "What about cost / token tracking?"
> Per agent, per provider, per tenant. Stored alongside the audit log,
> queryable from the dashboard or `/api/v1/usage`. Useful both for
> finance (which tenant is spending what) and for catching runaway
> agents (alert when an agent's token rate spikes).

### "How does this compare to running Claude Code / a Cursor-style
> agent on a bastion host?"
> Different shape of product. Claude Code is a personal-IDE agent —
> fantastic for that. Cyntr is the platform you put a *shared* agent
> on, with multi-tenant isolation and policy so the same `cloud-ops`
> agent can serve the platform team, the data team, and the SRE
> team without leaking permissions. If you're a team of one, Claude
> Code is the better answer.

### "Is the audit log queryable from outside the dashboard?"
> Yes — `GET /api/v1/audit?tenant=...&user=...&action=...&since=...`
> returns the slice, JSON or CSV export. Same endpoint your SIEM
> would scrape. Hash chain means you can detect retroactive edits
> if anyone tries to.
