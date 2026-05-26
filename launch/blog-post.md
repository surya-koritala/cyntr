# Cyntr: the open-source AI agent platform with Rego policy-as-code

> v1.1 launch post. Target length: ~2,200 words. Honest about what's
> shipped, what isn't, and who Cyntr is for.

---

We're open-sourcing Cyntr today: an Apache-2.0 platform for running AI agents inside a company. A single Go binary, SQLite for state, OPA Rego for policy, federation across independent nodes. If you've ever tried to take a LangChain prototype and turn it into "something the security team will let us put in front of a Slack workspace," this post is for you.

The TL;DR: most open-source agent frameworks are libraries — you import them and build the operational story yourself (auth, audit, multi-tenant, approvals, channels). Cyntr is the operational story. You deploy it; it runs your agents.

## The problem we kept running into

We've spent the last few years inside or alongside teams shipping agent-style products — internal copilots, customer-support bots, cloud-ops automations. The pattern was always the same.

Someone (often us) would build a prototype in a Jupyter notebook with LangChain or a hand-rolled tool-loop, demo it, and get a thumbs-up. Then it would hit reality:

- **Security wanted to know what the agent could do.** Not "what's in the prompt" — what tools were exposed, who could call them, under what conditions, with what logging.
- **Ops wanted to run it.** Which means: not a Python process you `nohup` on a VM, but something with health checks, structured logs, rate limits, a way to ship config without redeploying.
- **The business wanted more than one team to use it.** Which means: tenants. Which means: you cannot have agent definitions in code, you need them in a database with permissions.
- **Finance wanted to know what it cost.** Per agent, per provider, per tenant.

By the time you've built all that, you've built a platform. We kept watching teams reinvent it badly, in Python, on top of frameworks that weren't designed to be platforms. So we built one.

The five things we decided we'd care about more than anyone else were:

1. **Policy-as-code.** Not a checkbox in a UI. A real policy engine — OPA Rego — that security can review and version-control alongside Kubernetes admission policies.
2. **Federation.** Multiple Cyntr nodes, each with its own policies, agents delegating across the boundary, with the receiving node always deciding.
3. **Hard multi-tenancy.** Tenants are not a UI concept; they are an isolation primitive in the kernel.
4. **Single binary.** No Redis, no Postgres, no Kafka. SQLite, embedded.
5. **Eval-as-CI.** Agent quality regressions are caught the same way code regressions are.

The next sections walk through each of those, with the actual code paths and demos.

## What Cyntr does differently

### 1. Policy-as-code with OPA Rego

Most agent frameworks model security as "the system prompt says don't do bad things." Some add an allowlist of tool names. Almost none let a security team express policy the way they're used to.

Cyntr ships two policy backends. The first is a YAML rule format for the simple cases:

```yaml
rules:
  - name: deny-shell-global
    tenant: "*"
    action: tool_call
    tool: shell_exec
    decision: deny
    priority: 20

  - name: allow-shell-cloudops
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: cloud-ops
    decision: allow
    priority: 30
```

The second is OPA Rego, evaluated by the open-policy-agent library directly:

```rego
package cyntr.policy

default decision := "allow"

# Deny shell_exec to non-admin agents in production tenants.
decision := "deny" if {
  input.action == "tool_call"
  input.tool == "shell_exec"
  startswith(input.tenant, "prod-")
  input.agent != "admin"
}

# Require approval for any AWS write action.
decision := "require_approval" if {
  input.action == "tool_call"
  startswith(input.tool, "aws")
}
```

Both backends are evaluated on every tool call, every federation request, every approval gate — by the same `CheckRequest` interface. The point of Rego isn't novelty; it's that platform-engineering teams already know it, already have CI for it, and already trust it to gate Kubernetes admission. Reusing that muscle for "what your agents can do" is the right shape.

### 2. Federation: independent nodes, cross-node delegation

This is the feature we think nobody else in the OSS agent world is shipping, and it's also the easiest one to demo. The idea: you run multiple Cyntr nodes — each with its own tenants, agents, LLM keys, and policies — and agents on one node can delegate work to agents on another.

The receiving node always decides whether to honour the request, because its local policy engine runs on every inbound federation call before the agent runtime is touched.

Here's the demo, lifted directly from `demos/federation/`. Two nodes — `node-a` (research org) and `node-b` (legal org). `node-b`'s policy:

```yaml
- name: allow-federated-legal
  tenant: legal
  action: federation_inbound
  agent: legal
  decision: allow
  priority: 50

- name: deny-federated-other
  tenant: "*"
  action: federation_inbound
  agent: "*"
  decision: deny
  priority: 40
```

Run the demo:

```bash
$ ./demos/federation/run.sh
=== Cyntr Federation Demo ===
>> building cyntr binary...
>> binary at /…/cyntr/bin/cyntr

>> starting node-a on :7700
>> starting node-b on :7800
>> waiting for node-a...
>> waiting for node-b...

>> joining peers
   node-a peers: [{"name":"node-b","endpoint":"http://127.0.0.1:7800",...}]
   node-b peers: [{"name":"node-a","endpoint":"http://127.0.0.1:7700",...}]

>> node-a -> node-b: federation.delegate (research -> legal)
{"data":{"peer_id":"node-b","agent":"legal","content":"Default mock
 response","decision":"allow"}}

>> node-a -> node-b: federation.delegate (research -> NON-EXISTENT,
   expect denial)
{"error":{"code":"DELEGATE_FAILED","message":"remote peer node-b:
 federation_inbound denied by policy: matched rule
 \"deny-federated-other\""}}

=== Done. Logs in .run/{node-a,node-b}/cyntr.log ===
```

Two things to notice. The first call succeeds, and `decision: allow` tells you `node-b`'s policy explicitly authorised it — not that `node-a` was trusted to ask. The second call is rejected with a named rule, before the agent runtime ever runs. That's the trust model: the receiver decides.

We use this for three patterns we care about:

- **Cross-team / cross-BU deployments.** Each team runs its own Cyntr. The infra team's node has the cloud credentials; the product teams delegate to it under approval policy. The product teams never see the credentials.
- **Data residency.** A tenant whose data must stay in the EU gets pinned to the EU node. Agents elsewhere have to come to it, not pull its data out.
- **Centralised audit.** A SIEM node federates with every other Cyntr node and queries their audit indexes for a single timeline.

The runnable demo is in `demos/federation/`. If you've read this far you can be testing federation in under three minutes:

```bash
git clone https://github.com/surya-koritala/cyntr.git
cd cyntr
go test ./demos/federation/ -v   # in-process, no infra
# or
./demos/federation/run.sh        # two real cyntr processes
# or
cd demos/federation && docker compose up --build
```

### 3. Hard multi-tenancy

Tenants in Cyntr are an isolation primitive, not a UI concept. Each tenant has its own:

- Agent namespace (you can have a `bot` agent in `engineering` and a different `bot` in `finance`).
- Policy scope (rules can be tenant-scoped or wildcard).
- Audit slice (audit log is queryable per tenant; one tenant's auditor can't see another's).
- Rate limit budget (token-bucket on the proxy gateway).
- LLM provider config (one tenant can be on Claude; another on Ollama; another on Azure OpenAI).

All of that runs in a single process. We picked this shape because the "one container per tenant" pattern is operationally heavy for a 5-tenant deployment and operationally impossible past 50.

### 4. Single binary, SQLite, no external deps

```bash
go build -o cyntr ./cmd/cyntr
./cyntr init
./cyntr start
```

That's the installation. There is no Postgres requirement, no Redis, no message queue. SQLite handles sessions, memory, audit, knowledge base (FTS5 for RAG), and workflow history. There's a kernel IPC bus for inter-module communication; modules boot in dependency order and shut down gracefully in reverse.

This is a deliberate trade. SQLite caps out somewhere — we wouldn't run a 10,000-tenant deployment on a single SQLite file. But the SQLite ceiling is much higher than people assume, and the operational simplicity of a single binary is a wedge against the "you need a Kubernetes cluster to run our agent platform" pattern that's emerged. When you outgrow it, switch the audit store to Postgres (the interface is there); the other modules are mostly fine on SQLite for far longer than you'd expect.

### 5. Eval-as-CI

Agent platforms have a quiet quality problem: you change a prompt, you swap a model, you upgrade a provider SDK, and three weeks later someone notices the bot stopped answering a class of questions correctly. We treat this the same way we treat code regressions.

`cyntr eval` runs a directory of test cases against a Cyntr server and exits non-zero when the pass rate drops below a threshold. The case format is JSON:

```json
{
  "agent": "assistant",
  "tenant": "ops",
  "cases": [
    {"id":"c1","input":"What is 2+2?","expected_output":"4","match_mode":"contains"},
    {"id":"c2","input":"List S3 buckets","expected_tools":["aws"]}
  ]
}
```

Three match modes (`contains`, `exact`, `regex`), per-case scoring, JUnit output for CI. Exit code 1 if you regressed; exit code 2 on tool errors. Drop it into GitHub Actions, gate your model upgrades on it.

## Cyntr vs Hermes Agent vs Dify

We get this question a lot. Here's our honest read.

**Hermes Agent.** Consumer leader. 163k stars. Beautiful chat-first UX, huge plugin ecosystem, the obvious choice if you want a personal-assistant pattern for individuals. The plugin model and the UX-first design are real strengths. Where they don't go (and don't intend to, as far as we can tell): hard multi-tenant isolation, federation across nodes, policy-as-code that a security team would actually review. Different shape of product.

**Dify.** Excellent visual workflow builder, broad provider support, real momentum on the no-code agent path. Earlier on enterprise primitives — multi-tenant, RBAC, audit — though that's clearly improving. If your decision driver is "give the product team a drag-and-drop tool," Dify is a great pick. If it's "give the security team a policy file they can review in a PR," we want a shot.

**LangChain.** Different category — a library, not a platform. You can use LangChain *inside* a Cyntr deployment via a custom tool; they're complementary. We're not trying to replace LangChain for prototyping. We're trying to be where the prototype goes after it works.

## Architecture in one picture

```
                         ┌─────────────────────────────────────┐
                         │ CLI · Dashboard · REST API · SDK    │
                         └──────────────┬──────────────────────┘
                                        │
                         ┌──────────────▼──────────────────────┐
                         │              KERNEL                  │
                         │   IPC Bus · Config · Resources       │
                         └──────────────┬──────────────────────┘
                                        │
   ┌────────┬────────┬────────┬─────────┼─────────┬────────┬────────┬────────┐
   │        │        │        │         │         │        │        │        │
 Policy   Audit   Agent    Channel   Proxy     Skill    Fed.    Sched.  Workflow
 Engine   Logger  Runtime  Manager   Gateway   Runtime  Module  Module  Engine
   │        │        │        │         │         │        │        │        │
   ▼        ▼        ▼        ▼         ▼         ▼        ▼        ▼        ▼
 OPA      hash    Anthr/   Slack/    rate     skill    HTTP    cron    steps
 Rego     chain   OpenAI/  Teams/    bucket   catalog  +       parser  + retries
                  Gemini/  Discord/  per                       (pure   + human
                  Ollama   email/    tenant                    Go)     input
                           Telegram
```

Every component is a kernel module. Modules communicate over an in-process IPC bus with backpressure. They boot in dependency order, they shut down gracefully on SIGTERM, and they reload on SIGHUP without dropping requests. Tests cover 37 packages.

If you want the deep-dive, the kernel sources are in `kernel/` and the module sources are in `modules/`.

## What's next

We're not done. Things we know we owe you:

- **Curator v1.** Agent-builds-agent: describe the agent in natural language, Cyntr generates the system prompt, tool list, and skill assignments, then runs an eval pass before saving. This is the next thing we want to ship.
- **Hosted offering (try.cyntr.dev).** A free-tier sandbox so you can try Cyntr without `git clone`. It exists; it's small. The federation demo will move there too.
- **Vertical packs.** Curated bundles of agents + skills + policies for cloud-ops, customer-support, SRE-incident-response. The pack convention is already in `packs/` — what's missing is the catalog.
- **Skill marketplace breadth.** The plumbing is there; the catalog is small. We need community.
- **Better streaming.** Provider-level token streaming for Slack and the dashboard isn't great yet. On the roadmap.

We're shipping v1.1 today because the federation story, the policy story, the audit story, and the eval story all work. Curator and the hosted sandbox are not gating prerequisites for the people we think Cyntr is most useful to right now.

## Try it

Pick the path that matches how much time you have.

```bash
# 30 seconds — see federation working in-process
git clone https://github.com/surya-koritala/cyntr.git
cd cyntr
go test ./demos/federation/ -v

# 2 minutes — two real cyntr processes
./demos/federation/run.sh

# 5 minutes — set up a real agent with your own LLM
go build -o cyntr ./cmd/cyntr
./cyntr init   # 5-step wizard
./cyntr start
# open http://localhost:7700
```

And the asks:

- **Star the repo** if you want to see where this goes: https://github.com/surya-koritala/cyntr
- **Try the cloud-ops demo** — it's the most concrete one we have for "agent that does real work."
- **Join the Discord** (link in repo README) — that's where the design discussions happen.
- **File an issue** if you hit a real-world deployment friction. We're listening.

This is v1.1, not v3.0. We're early. We'd rather hear about the problems now while we can still shape the answers.

Apache 2.0. Built with Go. No frameworks, no managed dependencies, no surprises in the supply chain.

— The Cyntr team
