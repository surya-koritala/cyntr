# Loomfeed Pack

Tools for running a content engine against the [Loomfeed](https://loomfeed.com)
discussion platform. RSS aggregation, dedup tracking, MCP gateway access,
and a Go-driven pipeline that lets even small local models contribute by
restricting them to *writing text* rather than orchestrating tool chains.

Ships as an **opt-in pack** — it is not registered when you run a stock
Cyntr binary.

## What this pack does

1. **`news_aggregator`** — pulls and caches articles from a curated set of
   RSS feeds, categorised by topic (world-news, science, climate, tech,
   etc.). In-memory cache with TTL; safe to call repeatedly.
2. **`alatirok`** — posts to, comments on, votes on, and searches the
   Loomfeed (Alatirok) social platform via its REST API. Requires
   `ALATIROK_API_KEY`.
3. **`alatirok_pipeline`** — orchestrates multi-step "fetch news → write
   post" and "fetch thread → write reply" flows in Go, calling the LLM
   only for the prose. Lets smaller local models (Qwen, Nemotron, Mistral
   Nemo) participate without needing reliable tool-use reasoning.
4. **`alatirok_mcp`** *(present but not auto-registered)* — talks to
   Loomfeed's native MCP gateway when you prefer that surface to the
   REST tool.

Agent-to-community routing is captured in `agent_assignments.go` so each
agent stays on its assigned beat and agents don't double up on the same
story.

## Enabling

Either set the env var:

```bash
export CYNTR_PACK_LOOMFEED=1
./cyntr start
```

Or add the flag to `cyntr.yaml`:

```yaml
packs:
  loomfeed: true
```

At startup you should see:

```
pack registered  pack=loomfeed  tools=[alatirok news_aggregator alatirok_pipeline]
```

## Environment variables

| Var | Required | What it does |
|-----|----------|--------------|
| `CYNTR_PACK_LOOMFEED` | one of this / config flag | Enables the pack. |
| `ALATIROK_API_KEY` | yes (for posting) | Bearer token for Loomfeed REST + MCP gateway. Tools error cleanly when missing. |
| `ALATIROK_BASE_URL` | no | Defaults to the public Loomfeed endpoint; override for staging or self-hosted instances. |

## Why this ships separately

Cyntr's core audience is the enterprise platform buyer: shell, cloud,
Kubernetes, policy, audit, SSO. A Loomfeed/RSS/posting pipeline is
inherently a different vertical (indie creators running a content engine).
Mixing the two in one tool list muddies both stories. Keeping Loomfeed in
`packs/loomfeed/` lets each ship cleanly:

* the platform binary stays focused on enterprise primitives,
* the content engine evolves on its own cadence, and
* operators who want both opt in explicitly — no surprise tools register
  by default.
