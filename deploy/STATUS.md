# T2.5 Deploy Status

State at the end of the T2.5 implementation pass. Read this before
attempting to flip the DNS switch on `try.cyntr.dev`.

## What works end-to-end

| Component | Status | Notes |
|---|---|---|
| `deploy/Dockerfile.prod` | written; not built on dev host | Multi-stage, non-root, healthcheck, distroless-style. See "Docker build" below. |
| `deploy/docker-compose.hosted.yml` + Caddyfile | written; not exercised | Auto-TLS via Let's Encrypt baked in. |
| `deploy/fly.toml` + runbook | written; not deployed | Recommended path. ~$5-15/mo. |
| `deploy/render.yaml` | written | Free tier has no disk — bump to `starter` ($7/mo). |
| `deploy/railway.json` | written | Hobby plan credits cover ~one cyntr-try. |
| `deploy/hosted.cyntr.yaml.example` | **boot-tested** | Loads cleanly, `/api/v1/system/health` returns 200, all stores land in `${CYNTR_DATA_DIR}`. |
| `deploy/hosted.policy.yaml.example` | **boot-tested** | Validated by policy engine. |
| `cmd/cyntr/main.go`: `CYNTR_DATA_DIR` support | **shipped** | New `dataPath()` helper; default `.` preserves back-compat. 8 store files redirect: sessions/memory/audit/usage/quota/curator/knowledge_base/usermodel + scheduler_jobs.json. |

## What's NOT done (deferred / out of scope)

1. **Free-tier quotas are NOT in the YAML config.** The brief asked for
   `quotas: { tokens_per_day: 50000, ... }` in `hosted.cyntr.yaml`, but
   `kernel/config/schema.go` has no `Quotas` field. Wiring it in is a 10-
   line change but it ships *Go code*, which the brief flagged as
   out-of-scope ("if Cyntr's config doesn't already support X, document
   the gap"). Today the operator must POST quotas after first boot — see
   `deploy/auth-public.md §3`. **Filed as a gap, with the manual-step
   script.**

2. **No anonymous auth mode.** Cyntr's auth supports OIDC + static bearer
   token only. The hosted free-tier wants "anyone can use" with per-browser
   session identity. Worked around by: a single `public` tenant + per-IP
   rate limit at the proxy layer. Full analysis in
   `deploy/auth-public.md §2`.

3. **Retention TTLs (7d sessions / 30d memories) are NOT honored by the
   hosted config.** They're hardcoded to 90d/180d/365d at
   `cmd/cyntr/main.go:152-156`. Same shape of fix as quotas — surface as
   YAML, two minutes of work, blocked on "no new code outside what's
   strictly needed".

4. **Docker build NOT executed on this host.** Docker is installed but the
   sandbox can't reach `/var/run/docker.sock`. The Dockerfile.prod is
   written to match the existing Dockerfile pattern (which is known to
   work) — operator should run `docker build -f deploy/Dockerfile.prod -t
   cyntr:test .` once on a host with daemon access before pointing CI at
   it.

5. **`shell_exec` docker backend falls back to in-process when Docker is
   unreachable.** On boot test in this sandbox we saw:
   `WARN docker backend requested but docker is not available — falling
   back to in-process for all tenants`. **This is the single biggest
   security gotcha for the public deploy.** Make sure the production
   container has access to a Docker daemon (Fly.io: separate Machine for
   sandboxing; docker-compose: mount `/var/run/docker.sock` with caveats;
   or replace the docker backend with gVisor/Firecracker).

## Quick-start verification

```bash
# from repo root
go build ./...                                # clean
go build -o /tmp/cyntr ./cmd/cyntr            # ~3s on M-series
mkdir -p /tmp/cyntr-test && cd /tmp/cyntr-test
cp ~/repos/cyntr-wt-hosted/deploy/hosted.cyntr.yaml.example ./cyntr.yaml
cp ~/repos/cyntr-wt-hosted/deploy/hosted.policy.yaml.example ./policy.yaml
CYNTR_DATA_DIR=./data /tmp/cyntr start ./cyntr.yaml &
sleep 4
curl -fsS http://127.0.0.1:7700/api/v1/system/health | jq .
```

Expected: a JSON status object with `data.agent_runtime.Healthy: true` and
~14 module entries, all healthy. SQLite stores appear under `./data/`.

## Cost snapshot (recommended path = Fly.io)

| Scenario | $/mo |
|---|---|
| Idle (auto-stop, no traffic) | ~$0-2 |
| Trickle (50 sessions/day, auto-stop) | ~$5-10 |
| Always-on (`min_machines = 1`, 256 MB) | ~$5-7 |
| Always-on, 512 MB (recommended once real users hit chromium tool) | ~$10-15 |

Volume (1 GB) is $0.15/mo. Bandwidth: ~$0.02/GB outbound after the first
160 GB. TLS, custom domain, monitoring dashboard: free.
