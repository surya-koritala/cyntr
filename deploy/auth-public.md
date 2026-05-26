# Auth model for the hosted free-tier (`try.cyntr.dev`)

## TL;DR

Cyntr's existing auth (OIDC bearer-token verification + optional
`CYNTR_API_KEY` static bearer) is geared for **single-operator** or
**enterprise SSO** deployments. The hosted free-tier needs a third mode —
**anonymous-by-default** — and Cyntr does not ship one today. This document
describes the working approximation we're using for the beta and the
upstream changes that would make it first-class.

## 1. What we do today (the working approximation)

Three layers, no Cyntr code changes:

1. **Reverse proxy enforces per-IP rate limits.** Caddy (in docker-compose)
   or Fly's `http_service.concurrency.hard_limit` (on fly.io) returns `429`
   above the threshold. This is the only line of defense against a
   `for i in {1..10000}` curl attack.

2. **All requests land on `tenant: public`.** The hosted `cyntr.yaml` declares
   exactly one tenant. The policy file (`deploy/hosted.policy.yaml.example`)
   denies destructive tools (`http`, `file_write`, `send_message`,
   `send_notification`, `kubectl`, `aws`) and requires approval for
   `shell_exec`. The tenant boundary is the same one Cyntr uses for every
   other isolation decision — audit log scope, quota bucket, retention TTL.

3. **Quotas are bounded at the kernel level.** The quota module is registered
   at startup (cmd/cyntr/main.go:640) with persistence at
   `${CYNTR_DATA_DIR}/quota.db`. For the free-tier we set the `public`
   tenant's quota via the IPC bus once the kernel is up — see the
   "Quotas on boot" section below.

Effect: a malicious anonymous user can chat, but cannot send mail, cannot
shell out, cannot DDoS us (rate-limited at edge AND quota'd per-tenant),
and cannot write to disk on the host.

## 2. What we DON'T do (the gap)

- **No per-user identity.** Two anonymous browsers share the same `public`
  tenant. Their session IDs are distinct (cookie-issued per browser) but
  they pull from the same quota bucket — one heavy user can starve everyone.
- **No abuse-bound per-IP quota.** Caddy rate-limits at the request layer
  but doesn't know about Cyntr's token-budget concept. A user under the
  request cap can still burn 50k tokens in a single long completion.
- **No turnstile / captcha / signup flow.** The first-touch UX is "open the
  page, type into the box" — no friction is part of the value prop, but it
  also means we can't ban a "user" because there's no user concept.

## 3. Quotas on boot (the manual step)

Until cyntr.yaml has a top-level `quotas:` section, the operator must POST
the free-tier limits to the quota IPC endpoint immediately after start. A
small shim script:

```bash
# deploy/scripts/set-hosted-quotas.sh
curl -fsS -XPOST http://127.0.0.1:7700/api/v1/quotas \
  -H "Authorization: Bearer ${CYNTR_API_KEY}" \
  -H "Content-Type: application/json" \
  -d '{
    "tenant":                  "public",
    "tokens_per_day":          50000,
    "requests_per_minute":     10,
    "max_concurrent_agents":   5,
    "max_sessions_per_day":    20
  }'
```

Wire this into a `postStart` hook in fly.io, or run it once after
`docker compose up`. The values persist in `quota.db` so a single
invocation is enough across restarts.

## 4. What would make this first-class

Three small upstream changes — none big enough for T2.5, but each worth
filing as an issue:

1. **`kernel/config/schema.go`: add `Quotas map[string]QuotaConfig`.**
   Load it in cmd/cyntr/main.go right after `k.Register(quota.New(...))`
   and call `enforcer.SetConfig(...)` for each entry. Eliminates §3 above.

2. **`auth: provider: anonymous` mode.** Issues a session cookie tying each
   browser to a generated `user_id` under `tenant: public`. The quota module
   already supports per-user accounting via the IPC `user_id` field — it's
   just not wired through anonymous paths.

3. **Retention TTL config in cyntr.yaml.** `sessions: ttl: 7d`,
   `memories: ttl: 30d`. Today these are hardcoded at lines 152-156 of
   cmd/cyntr/main.go. Two minutes of work to expose; we don't have a
   user-facing knob for it yet because the existing single-operator use
   case has no reason to want shorter TTLs.

## 5. Security checklist before pointing `try.cyntr.dev` DNS

- [ ] `CYNTR_API_KEY` set to a long random string (gates the dashboard).
- [ ] Caddy/Fly per-IP rate limit active (verify with `ab -n 200 -c 10`).
- [ ] Hosted policy file mounted at `/cyntr/policy.yaml`.
- [ ] `shell_exec_policies.public.backend == "docker"` AND `docker info`
      works from inside the container. If docker is unavailable, Cyntr
      logs a WARN and falls back to in-process — **stop and fix this**
      before going public.
- [ ] Quotas POSTed via §3 script (or manually via dashboard) — confirm
      via `GET /api/v1/quotas/public`.
- [ ] Audit log destination is the persistent volume, not `/tmp`.
- [ ] OTel exporter pointed at an off-box collector (so logs survive
      machine restarts).
