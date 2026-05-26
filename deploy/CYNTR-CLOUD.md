# `try.cyntr.dev` operator runbook

Surya — this is the actual day-zero / day-one / day-thirty document for
running `try.cyntr.dev` (and eventually `cyntr.cloud`). Skip the README;
this is the page to keep open in a tab.

## Day 0 — DNS, TLS, first deploy

1. **Pick the platform.** Default = Fly.io. If you want full control of
   the host (e.g., to mount a Docker socket so the `shell_exec` sandbox
   actually sandboxes), use the docker-compose path on a Hetzner CX22.

2. **Reserve the IPs.** On Fly: `fly ips list -a cyntr-try` after the
   first `fly launch`. On self-host: your VPS's static IPv4/IPv6.

3. **DNS.** At the registrar (Cloudflare? gandi? wherever cyntr.dev is):
   ```
   try.cyntr.dev    A    <ipv4>
   try.cyntr.dev    AAAA <ipv6>
   ```
   TTL 300 while you're iterating, bump to 3600 once stable. **Do NOT
   proxy through Cloudflare orange-cloud** until you've confirmed:
   (a) websocket / SSE for the dashboard live-events tab works, and
   (b) Anthropic's Authorization header isn't being mangled.

4. **TLS.** Fly issues automatically via `fly certs create`. Self-host: Caddy
   handles ACME inside `docker-compose.hosted.yml` — make sure ports 80
   and 443 are open on the host firewall.

5. **First deploy.** Follow `deploy/fly-launch.md` end-to-end. Verify with:
   ```bash
   curl -fSs https://try.cyntr.dev/api/v1/system/health
   ```

6. **Set quotas** (manual step until `kernel/config/schema.go` learns
   about quotas — see `STATUS.md` §1):
   ```bash
   curl -fsS -XPOST https://try.cyntr.dev/api/v1/quotas \
     -H "Authorization: Bearer ${CYNTR_API_KEY}" \
     -H "Content-Type: application/json" \
     -d '{"tenant":"public","tokens_per_day":50000,"requests_per_minute":10,"max_concurrent_agents":5,"max_sessions_per_day":20}'
   ```

7. **Smoke test the chat path:**
   ```bash
   curl -X POST https://try.cyntr.dev/api/v1/chat \
     -H "Authorization: Bearer ${CYNTR_API_KEY}" \
     -d '{"tenant":"public","agent":"assistant","input":"Say hi"}'
   ```

## Day 1 — monitoring + uptime

1. **UptimeRobot.** Free plan, 5-min checks. Two monitors:
   - HTTPS GET `https://try.cyntr.dev/api/v1/system/health` — alert on
     non-200 or `data.agent_runtime.Healthy != true`.
   - Plain TCP on 443 — catches platform-level outages even when cyntr
     itself is up.

2. **Alert routing.** UptimeRobot → Slack webhook. The webhook URL goes
   in the alert contact, not in any cyntr config. Recommend a dedicated
   `#cyntr-cloud-alerts` channel so the noise doesn't drown out feature
   work.

3. **Logs.** Fly: `fly logs -a cyntr-try -f` from your laptop, or pipe
   structured logs to BetterStack/Logtail. Self-host: Caddy + cyntr both
   log to stdout in JSON; `docker logs -f` is fine for the beta. For
   anything serious, ship to a remote sink (the on-box logs vanish when
   the container is replaced).

4. **Metrics.** If `OTEL_EXPORTER_OTLP_ENDPOINT` is set, Cyntr exports
   traces and metrics via OTLP-HTTP. Point at:
   - Honeycomb (free 20 GB/mo) — easiest for traces.
   - Grafana Cloud Free — easiest for metrics + log correlation.
   - Self-hosted Tempo+Mimir — most control, most ops work.

## Day 7 — first abuse incident

What will go wrong first: someone discovers the public endpoint and writes
a script that opens N sessions and burns the daily token budget in 90s.

**Detection.** UptimeRobot won't catch it (the endpoint is still up).
You'll see it via:
- Sudden spike in Anthropic spend.
- `GET /api/v1/quotas/public` returns 100% of `tokens_per_day` used.
- Audit log shows N sessions from a small number of IPs.

**Response.**
1. Drop the quota: `tokens_per_day: 5000` to slow them down.
2. Identify the abusing IPs from the audit log:
   ```bash
   fly ssh console -a cyntr-try -C "sqlite3 /cyntr/data/audit.db \
     'SELECT remote_ip, COUNT(*) FROM events WHERE ts > unixepoch()-3600 GROUP BY remote_ip ORDER BY 2 DESC LIMIT 5'"
   ```
3. Block at the edge. Fly: `fly machine update --restart-on-failure no
   --command "iptables -A INPUT -s <ip> -j DROP"` is wrong — instead add
   a Caddy block list, or use Fly's network policies. Self-host: Caddy's
   `@bad client_ip <ip>` matcher → `respond 429`.

## Day 30 — backups + cost review

1. **Weekly SQLite backup to S3.** Add to fly cron (or self-host cron):
   ```bash
   # /etc/cron.weekly/cyntr-backup
   #!/bin/bash
   set -e
   ts=$(date -u +%Y%m%d-%H%M%S)
   cd /cyntr
   ./cyntr backup /tmp/cyntr-${ts}.tar.gz
   aws s3 cp /tmp/cyntr-${ts}.tar.gz s3://cyntr-cloud-backups/try/${ts}.tar.gz \
     --storage-class STANDARD_IA
   rm /tmp/cyntr-${ts}.tar.gz
   # Retain 90 days
   aws s3api list-objects-v2 --bucket cyntr-cloud-backups --prefix try/ \
     --query "Contents[?LastModified<='$(date -u -d '90 days ago' -Iseconds)'].Key" \
     --output text | xargs -r -n1 -I {} aws s3 rm s3://cyntr-cloud-backups/{}
   ```
   On Fly the easiest path is a sidecar Machine running this cron, with
   the volume mounted read-only.

2. **Restore drill.** Every 90 days:
   ```bash
   aws s3 cp s3://cyntr-cloud-backups/try/<latest>.tar.gz /tmp/
   fly ssh console -a cyntr-try-staging
   # inside container:
   /usr/local/bin/cyntr restore /tmp/<latest>.tar.gz
   curl -fSs http://127.0.0.1:7700/api/v1/system/health
   ```

3. **Cost review.** Pull last 30 days of:
   - Anthropic API spend (token usage * model rate).
   - Fly compute + bandwidth (`fly billing show`).
   - S3 storage (~$0.01/GB-mo for backups).

   Sanity-check against the quotas: if Anthropic spend is below the
   `tokens_per_day * 30 * model_rate` ceiling, quotas are working. If
   above, look for misconfigured quotas.

## Incident response — rollback

Cyntr is a single binary + a SQLite directory. Rollback is just: deploy
the previous image, optionally restore the data dir.

```bash
# 1. Find the previous good release:
fly releases -a cyntr-try

# 2. Roll back compute:
fly deploy --image registry.fly.io/cyntr-try:<previous-tag> -a cyntr-try

# 3. If data is corrupt too (rare — SQLite is robust), restore from
#    S3 backup BEFORE the machine restarts:
aws s3 cp s3://cyntr-cloud-backups/try/<known-good>.tar.gz /tmp/
fly ssh console -a cyntr-try
cyntr restore /tmp/<known-good>.tar.gz

# 4. Verify:
curl -fSs https://try.cyntr.dev/api/v1/system/health
```

**Never** restore from a backup older than 24h without thinking carefully —
you'll lose sessions/memory accumulated between the backup and now, and
that's the actual interesting bit for an early-stage product.

## Pre-launch security checklist

Don't point DNS at try.cyntr.dev publicly until every box is checked.
This list is duplicated from `auth-public.md §5`; bring it forward here
so you can blast through it from this single page.

- [ ] `CYNTR_API_KEY` set, dashboard auth gated.
- [ ] Per-IP rate limit verified (`ab -n 200 -c 10 https://try.cyntr.dev/`).
- [ ] `shell_exec_policies.public.backend = docker` AND Docker daemon
      reachable inside the cyntr container. **Test this end-to-end.** If
      the WARN-fallback to in-process triggers in production, anonymous
      users can shell-out on your host.
- [ ] Quotas POSTed and confirmed via `GET /api/v1/quotas/public`.
- [ ] Audit log writing to the persistent volume (verified with `ls -la
      /cyntr/data/audit.db` over `fly ssh`).
- [ ] OTel exporter live and receiving traces (Honeycomb dataset showing
      events).
- [ ] UptimeRobot monitors green.
- [ ] Backup cron tested with a restore drill.
- [ ] Domain TLS valid (`curl -vI https://try.cyntr.dev/ 2>&1 | grep
      "certificate verify"`).

When all eight are green, flip DNS.
