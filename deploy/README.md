# `deploy/` — production deployment artifacts for Cyntr

Production deployment artifacts for Cyntr. Pick a platform, follow the
runbook, ship a hosted Cyntr endpoint.

## Three deployment paths

| Platform | Best for | $/mo | Setup time |
|---|---|---|---|
| **Fly.io** (recommended) | beta + small-prod, global anycast | $5-15 | 15 min ([`fly-launch.md`](./fly-launch.md)) |
| **Render** | simplest YAML-only flow, free CI | $7+ (disk required) | 10 min ([`render.yaml`](./render.yaml)) |
| **Railway** | nicest dashboard, hobby credits | $5 | 5 min ([`railway.json`](./railway.json)) |
| Self-host (docker-compose + Caddy) | full control, your domain, your TLS | $6-12 (Hetzner/DO) | 30 min ([`docker-compose.hosted.yml`](./docker-compose.hosted.yml)) |

Fly is the recommended default — it auto-stops idle machines (compute=$0
when no one's hitting the site), handles TLS automatically, and the cold
start (~3s) is acceptable for a "try it" landing page.

## Files

```
deploy/
├── README.md                       ← this file
├── STATUS.md                       ← what works, what's deferred (read first)
├── CYNTR-CLOUD.md                  ← Surya's runbook for operating try.cyntr.dev
├── Dockerfile.prod                 ← multi-stage, non-root, healthcheck
├── docker-compose.hosted.yml       ← Caddy + cyntr + (optional) jaeger/prom
├── Caddyfile                       ← reverse proxy + auto-TLS + per-IP rate limit
├── hosted.env.example              ← every env var documented
├── hosted.cyntr.yaml.example       ← free-tier cyntr config (boot-tested)
├── hosted.policy.yaml.example      ← deny destructive tools on public tenant
├── auth-public.md                  ← gap analysis: anonymous auth mode
├── fly.toml                        ← fly.io manifest
├── fly-launch.md                   ← fly.io step-by-step launch runbook
├── render.yaml                     ← Render Blueprint
└── railway.json                    ← Railway template config
```

## Pre-flight checklist

Before deploying to any platform:

- [ ] At least one LLM provider key (`ANTHROPIC_API_KEY` recommended).
- [ ] `CYNTR_API_KEY` set to a long random string while the beta is private.
- [ ] Volume size ≥ 1 GB (SQLite stores live there).
- [ ] DNS A/AAAA records ready to point at the platform's IPs.
- [ ] (Self-host only) Docker daemon reachable from inside the cyntr
      container — see [`auth-public.md §5`](./auth-public.md) for why this
      matters.
- [ ] Hosted config + policy file present at `cyntr.yaml` + `policy.yaml`
      paths inside the container.

## Post-deploy verification

```bash
# 1. Health check
curl -fSs https://<your-domain>/api/v1/system/health | jq .data.agent_runtime
# expect: {"Healthy":true,"Message":"0 agents running"}

# 2. Smoke chat (after setting ANTHROPIC_API_KEY)
curl -X POST https://<your-domain>/api/v1/chat \
  -H "Authorization: Bearer ${CYNTR_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{"tenant":"public","agent":"assistant","input":"Say hi in 5 words"}'

# 3. Run an eval suite (CI-friendly):
cyntr eval https://<your-domain> evals/*.json
```

## Cost estimates (free-tier `public` workload)

| Platform | Idle | 50 sessions/day | 500 sessions/day |
|---|---|---|---|
| Fly.io (shared-cpu-1x, auto-stop) | $0-2 | $5-10 | $15-30 |
| Render (starter) | $7 | $7-12 | $20-40 |
| Railway (hobby) | $5 | $5-15 | $25-50 |
| Self-host (Hetzner CX22) | $6 | $6 | $6 (until you OOM) |

The hosted free-tier ships with quotas of 50k tokens/day across the whole
`public` tenant. At those limits, Anthropic API spend tops out at ~$3/day —
add that to platform compute and you have a hard ceiling per environment.

## Read next

- [`STATUS.md`](./STATUS.md) — what's working, what's deferred
- [`fly-launch.md`](./fly-launch.md) — the recommended deploy path
- [`auth-public.md`](./auth-public.md) — what the auth model actually is
- [`CYNTR-CLOUD.md`](./CYNTR-CLOUD.md) — operator runbook (DNS / TLS /
  monitoring / backups / incident response)
