[Cyntr Docs](../README.md) > How-to > Deploy to Fly.io

# Deploy to Fly.io

A worked example for a small production deploy. Cyntr fits naturally on Fly because it's a single binary with SQLite — no separate database to provision.

Prerequisites: `flyctl` installed, a Fly account, your provider API keys at hand.

## 1. Generate the Fly config

```bash
flyctl launch --no-deploy
```

Pick a name, a region, and **decline** the offer to provision Postgres or Redis — Cyntr doesn't need them.

## 2. Adjust `fly.toml`

```toml
app = "cyntr-yourname"
primary_region = "iad"

[build]
  dockerfile = "Dockerfile"

[env]
  CYNTR_LOG_LEVEL = "info"

[[services]]
  internal_port = 7700
  protocol = "tcp"

  [[services.ports]]
    handlers = ["http", "tls"]
    port = 443

  [services.http_checks]
    interval = "10s"
    method = "get"
    path = "/api/v1/system/health"

[[mounts]]
  source = "cyntr_data"
  destination = "/data"
```

The mount is critical — SQLite files live there. Without it, every deploy wipes your audit log.

## 3. Create the volume

```bash
flyctl volumes create cyntr_data --size 10
```

10GB is plenty for ~6 months of moderate usage. Resize later with `flyctl volumes extend`.

## 4. Set secrets

```bash
flyctl secrets set \
  ANTHROPIC_API_KEY=sk-ant-... \
  CYNTR_API_KEY=cyntr_$(openssl rand -hex 16) \
  SLACK_BOT_TOKEN=xoxb-... \
  SLACK_SIGNING_SECRET=...
```

Secrets are encrypted at rest and injected as env vars at boot.

## 5. Deploy

```bash
flyctl deploy
```

First deploy takes ~2 minutes (Go build + image push). Subsequent deploys are <60s.

## 6. Verify

```bash
flyctl status
curl https://cyntr-yourname.fly.dev/api/v1/system/health \
  -H "X-API-Key: $CYNTR_API_KEY"
```

## Scale notes

- **Vertical first.** Bump VM size (`flyctl scale vm shared-cpu-2x`) before adding replicas. Cyntr's bottleneck at low scale is almost always LLM API latency, not compute.
- **Multiple replicas with SQLite is tricky.** For 2+ replicas, either pin one as primary or migrate sessions/audit to Postgres. Talk to us if you hit this — it's the storage abstraction story we're still hardening.

## Related

- [Getting started: Deploy](../getting-started/deploy.md) — production checklist.
- [Reference: Env vars](../reference/env-vars.md)
