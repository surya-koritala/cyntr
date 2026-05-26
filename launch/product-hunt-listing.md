# Product Hunt listing — Cyntr

> Drafted for a Tuesday 12:01 AM PT launch. Replace `[discord]` and
> `[hosted-sandbox]` URLs at launch time.

## Tagline

```
Self-hosted AI agent platform with policy and federation
```

(57 chars, under the 60-char PH cap.)

## Description — short (300 chars)

```
Cyntr is an open-source AI agent platform you self-host as a single Go
binary. OPA Rego policy-as-code, SHA-256 audit chains, federation across
nodes, multi-tenant. Apache 2.0. Built for teams who want to put agents
into production without rebuilding the boring parts.
```

(297 chars.)

## Description — long (1500 chars)

```
Cyntr is the open-source AI agent platform we wish existed when we
started shipping agent products inside companies. It's a single Go
binary, SQLite for state, zero external services. You deploy it; it
runs your agents.

The two things we built it around:

1. Policy-as-code with OPA Rego. Every tool call, every federation
   request, every approval gate is evaluated by the same policy
   engine. Security teams review agent permissions the same way they
   review Kubernetes admission policies — in a PR.

2. Federation across independent nodes. Multiple Cyntr instances —
   each with its own tenants, agents, and LLM keys — can have their
   agents delegate work across the boundary. The receiving node's
   policy always decides. Try it: `go test ./demos/federation/ -v`.

Also included: multi-tenant isolation in one process, OIDC/SSO,
SHA-256 audit hash chains, PII detection, secret masking, an
evaluation framework that runs in CI (exit code 1 on regression),
8 LLM providers (Claude, GPT, Gemini, Ollama, …), 9 channels
(Slack, Teams, Discord, …), 8 built-in MCP servers, a 17-page
dashboard, Python + JS SDKs.

This is v1.1. The Curator (agent-builds-agent) is on the roadmap, the
skill catalog is small but growing, the hosted sandbox is free.

Apache 2.0. github.com/surya-koritala/cyntr
```

(1,494 chars.)

## Gallery shots — describe to the designer

Six images, 1270×760, dark mode, JetBrains Mono labels. No people,
no stock illustration, no gradients.

1. **Hero — federation diagram.** Two nodes (node-a / node-b), one
   arrow labelled `/federation/delegate`, the receiving side labelled
   `policy.check → allow`. Caption underneath: "The receiver always
   decides." This is the differentiation shot; it goes first.

2. **Terminal — federation demo running.** A real screenshot of
   `./demos/federation/run.sh` output, with the `decision: allow` line
   highlighted and the second denied call underneath it with the
   error message visible. Caption: "Run `./demos/federation/run.sh`
   in two minutes."

3. **Dashboard — Agents page.** The agents list with `cloud-ops`,
   `assistant`, `incident-team` visible, the chat panel open on the
   right showing a streamed response. Caption: "17-page dashboard.
   No frontend setup."

4. **Policy file side-by-side.** Left: YAML rules. Right: equivalent
   Rego. Caption: "Choose your policy language. Same evaluator."

5. **Eval CI screenshot.** Terminal output of `cyntr eval ./evals/`
   with three passes and one fail, exit code 1, plus a GitHub Actions
   green-then-red transition. Caption: "Regressions fail the build."

6. **Architecture sketch.** The kernel + 9 modules diagram from the
   README, dark mode. Caption: "Single binary. Thirteen modules. One
   SQLite file."

## Reply templates — typical PH comments

These are ready to paste with light editing on launch day.

### "Looks great — how does this compare to Hermes Agent / Dify?"
> Thanks! Quick honest read: Hermes is the consumer leader and excellent
> at chat-first UX with a huge plugin ecosystem; Dify has a really
> strong visual workflow builder and a head start on no-code agents.
> Cyntr's bet is different — we're going hard at the boring enterprise
> stuff: OPA Rego policy-as-code, hard multi-tenant isolation, federation
> across independent nodes, audit hash chains, eval-as-CI. Different
> shape of product for a different decision-maker. If you're picking
> by "best visual builder," Dify is the answer. If you're picking by
> "what will the security team approve," try Cyntr.

### "Is this just LangChain in Go?"
> Different category, actually. LangChain is a library — you import it,
> write a program, deploy that program, and build all the operational
> stuff yourself. Cyntr is the deploy target — you run `./cyntr start`
> and you get tenants, audit, RBAC, OIDC, policy, scheduler, workflows,
> 9 channel adapters, dashboard. LangChain is great for prototyping;
> we're built for what happens after the prototype works. You can also
> use them together — call into Cyntr from a LangChain app via the
> REST API and use it as your audit + policy + channel layer.

### "Why Go and not Python?"
> Three reasons. (1) Single static binary — no Python version drift on
> customer machines. Huge for self-hosted enterprise. (2) The boring
> parts (HTTP server, SQLite, audit, scheduler, policy evaluator,
> federation transport) are easier in Go; LLM calls are just HTTP either
> way. (3) OPA's reference implementation is in Go and embeds cleanly.
> We ship a Python SDK and a YAML tool format so agent authors don't
> have to touch Go to ship a new capability.

### "Does it work with local models?"
> Yes — Ollama is a first-class provider. Set `OLLAMA_URL=http://localhost:11434`
> in your `.env`, pick a model in the dashboard, done. Same agent code,
> same skill system, same policy engine. We test against Llama and
> Mistral.

### "What's the trust model on federation?"
> Receiver decides. Every inbound federation call is evaluated by the
> receiving node's local policy engine *before* the agent runtime sees
> it. The sender can ask for anything; only the receiver's policy
> controls whether it's honoured. The demo at `demos/federation/` has
> a denied call you can see in the output.

### "Is the hosted version going to stay free?"
> The free tier is a sandbox — small rate limits, mock provider, no
> stored data. It's there so you can poke at federation and policy
> without running the binary locally. We'll have a paid hosted tier
> later for teams who want managed Cyntr; the self-hosted binary stays
> Apache 2.0 forever.

### "Will you support [specific channel / provider / tool]?"
> Probably yes. Channels: PRs welcome — the adapter interface is small.
> Providers: same — `modules/provider/` is the place. Tools: you can
> add a YAML tool without any Go in 30 seconds (see README → Custom
> YAML Tools). Open an issue with the specifics and we'll point you at
> the right file.

## Maker comments — 3 drafts for launch-time posting

### Maker comment 1 — at launch (the "why we built it")
> Hey PH 👋 (We're not using emojis but PH culture seems to expect one
> wave, so: hi.)
>
> We built Cyntr because we kept watching teams reinvent the same agent
> platform — badly, in Python, on top of frameworks that weren't
> designed to be platforms. The pattern: someone ships an agent
> prototype, then spends three months bolting on auth, multi-tenant,
> audit, channels, policy, eval. By the time it's in production they've
> built a platform; they just didn't notice.
>
> So we built the platform. Single Go binary, OPA Rego policy,
> federation across nodes, SQLite for everything. Apache 2.0.
>
> The federation demo is the most concrete proof of differentiation —
> if you have a Go toolchain handy you can run it in under two minutes:
> `git clone … && go test ./demos/federation/ -v`.
>
> Happy to answer anything — particularly interested in feedback from
> teams who've actually deployed agents at work and hit the operational
> wall.

### Maker comment 2 — mid-day (the "what's coming")
> Quick what's-on-the-roadmap update since a few people have asked:
>
> – Curator v1: describe an agent in natural language, Cyntr generates
>   the prompt + tool list + skill assignments and runs an eval pass
>   before saving. Next thing we want to ship.
> – Vertical packs: cloud-ops, customer-support, SRE-incident. The
>   `packs/` convention is already there; what's missing is curated
>   bundles for each.
> – Better streaming: provider-level token streaming in Slack and the
>   dashboard isn't great yet.
> – Skill marketplace breadth: the plumbing is there, the catalog is
>   small. This is where we need community.
>
> If you have a specific use-case that's currently a pain — drop it
> in an issue, we read every one.

### Maker comment 3 — end of day (the "thank you")
> Thanks everyone who tried it today. Lessons we're taking back from
> the comments: docs need a "Cyntr in 60 seconds" page; the federation
> demo needs a recorded version for people who don't have a Go
> toolchain handy; we need a clearer comparison-to-Hermes paragraph.
>
> All three are on the list for this week. The repo, the docs, and
> the hosted sandbox stay up — and we'll be in the Discord all week
> answering deployment questions. Star us if you haven't; it really
> does matter for visibility on these things.
>
> Talk soon.
