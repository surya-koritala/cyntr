# r/selfhosted launch post

> Post Wednesday around 10 AM Pacific — a day after HN, so the comment
> can reference "we got X stars yesterday on HN" if it went well.
> r/selfhosted is allergic to marketing and loves: zero-dep installs,
> docker-compose files, real screenshots of self-hosting it. Lean into
> all three.

---

## Title

```
[Release] Cyntr v1.1 — open-source AI agent platform, single Go binary,
self-hosted, no SaaS, Apache 2.0
```

(Note: title is 122 chars — well under Reddit's 300-char cap. The
`[Release]` tag is conventional for the sub.)

## Body

```
Hi r/selfhosted —

We've been building an open-source AI agent platform for the last few
months and v1.1 is finally in a state we're comfortable showing.

Why this might be interesting to this sub specifically:

– Single Go binary. ~40 MB. No Python venv, no Node, no Java.
– SQLite is the only data store. No Postgres, no Redis, no Kafka.
– `docker-compose up` or `./cyntr start` — pick one.
– All eight LLM providers (Claude, GPT, Gemini, Ollama, ...) are
  configurable per-tenant. Ollama works fine on the local network if
  you don't want any cloud calls leaving the box.
– OIDC/SSO if you want to wire it into your Authelia/Authentik.
– SHA-256 audit hash chains, PII detection, secret masking — actually
  useful when you give an agent access to your home lab tooling.
– Apache 2.0. No CLA. No "open core" trap.

What it does: runs AI agents that can talk to Slack/Teams/Discord/
Telegram/email, run shell commands and kubectl (with policy
enforcement), query databases, search a built-in knowledge base
(SQLite FTS5, no external vector DB), and call other agents on
federated Cyntr nodes. There's a 17-page dashboard at
http://localhost:7700 for the boring management stuff.

Quick start:

  git clone https://github.com/surya-koritala/cyntr.git
  cd cyntr
  go build -o cyntr ./cmd/cyntr
  ./cyntr init
  ./cyntr start

If you don't want to install Go:

  docker compose up

The federation demo (`demos/federation/`) is the most interesting bit
if you've got 2 minutes — two nodes, one cross-node delegation, the
receiver's policy gets to deny. Runs in-process with `go test`.

Honest caveats (it's v1.1):

– Skill catalog is small — about 12 skills shipped, more from the
  community in the pipeline.
– Provider-level token streaming isn't great yet for Slack/dashboard.
– No clustering / no HA story — single node per cluster, federation
  for cross-node patterns.

Repo: https://github.com/surya-koritala/cyntr
Federation demo: https://github.com/surya-koritala/cyntr/tree/main/demos/federation
Docker: https://github.com/surya-koritala/cyntr/blob/main/docker-compose.yml

Happy to answer deployment questions — particularly interested if you
hit a friction point getting it running on Unraid / Synology / a
Pi cluster / wherever you self-host.
```

## Comment to add 30 seconds after posting

> Quick clarification before someone asks: there's no telemetry, no
> phone-home, no anonymous-usage-pings, no auto-update checker. The
> binary is offline-capable end-to-end as long as you use Ollama as
> the provider. Audit log lives in `./cyntr.db` (SQLite), back it up
> with `cp` like everything else.

## Reply templates

### "Does it need a GPU?"
> Only if you're running Ollama locally and want it to feel fast. Cyntr
> itself is a Go process — tens of MB of RAM idle, no GPU required.
> If you point it at Claude or GPT, no local compute at all.

### "How is it different from [other selfhosted agent thing]?"
> Probably: hard multi-tenant, OPA Rego policy, federation across
> nodes, audit hash chains. Most selfhosted agent projects we've seen
> are single-user or rely on you having a Postgres + Redis stack. The
> trade-off is we don't have as many flashy integrations as some — if
> you want a one-click Home Assistant agent today, we're not it yet.

### "Will it run on a Raspberry Pi?"
> The binary itself, yes — go build -o cyntr ./cmd/cyntr from a Pi 4
> or 5 works. The bottleneck is your LLM provider; running Ollama
> locally on a Pi is slow but functional with a small model. We have a
> few users running it on a Pi 5 with the cloud providers, no
> complaints.

### "Is there a Helm chart?"
> Not yet. The single-binary deployment was deliberate — we wanted
> Cyntr to feel like sqlite or caddy, not like a Kubernetes-first
> product. We'll likely ship a chart once a user actually asks for
> one. If that's you: file an issue.
