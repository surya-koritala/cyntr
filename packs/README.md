# Cyntr Packs

A **pack** is a coherent, opt-in set of agent tools that ships in the Cyntr
binary but is **not registered by default**. Packs let Cyntr stay a focused
enterprise agent platform while still distributing vertical capabilities
(content publishing, social automation, domain-specific scrapers, etc.) for
operators who want them.

If you only need shell, web, cloud, Kubernetes, data analysis, and the rest
of the core toolset, you can ignore packs entirely. Nothing in `packs/`
registers itself unless you ask for it.

## Enabling a pack

Two equivalent switches — pick whichever fits your deployment:

1. **Environment variable** — `CYNTR_PACK_<NAME>=1` (e.g.
   `CYNTR_PACK_LOOMFEED=1`). Useful for container deployments and one-off
   runs.

2. **Config flag** — under `packs:` in `cyntr.yaml`:

   ```yaml
   packs:
     loomfeed: true
   ```

   Useful when you manage Cyntr through Git-tracked config.

Either form is sufficient. Both default to *off*.

When a pack is enabled at startup, you will see a log line like:

```
pack registered  pack=loomfeed tools=[alatirok news_aggregator alatirok_pipeline]
```

## Available packs

| Pack | Purpose | Audience |
|------|---------|----------|
| [`loomfeed`](./loomfeed/README.md) | RSS aggregation, Firecrawl fetch, dedup, LLM write-up, and posting to the Loomfeed social platform. | Indie creators / content engines |

## Writing your own pack

Packs are ordinary Go packages under `packs/<name>/` that expose constructors
returning types implementing `agent.Tool` (`Name`, `Description`,
`Parameters`, `Execute`). Conventions:

1. **Directory** — `packs/<name>/`. One package, package name matches the
   directory.
2. **Constructors** — `New<Tool>Tool()` returning `*<Tool>Tool`. Tools may
   call back into shared infrastructure (`agent.ToolCaller`, the IPC bus)
   exactly like core tools.
3. **Registration** — add a guarded block in `cmd/cyntr/main.go` using
   `packEnabled(cfg.Packs, "<name>", "CYNTR_PACK_<NAME>")`. Never register
   pack tools unconditionally.
4. **README** — every pack ships a `README.md` describing what it does,
   which env vars it needs, and why it ships outside core.
5. **No new `go.mod` deps without review** — packs share the platform's
   dependency surface; new deps should land via a normal review.
6. **Tests live with the pack** — `go test ./packs/<name>/` must pass on its
   own.

See `packs/loomfeed/` for a worked example.
