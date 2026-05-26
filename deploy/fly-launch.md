# Fly.io launch runbook for `try.cyntr.dev`

End-to-end, copy-pasteable. Assumes you have `flyctl` installed and
`fly auth login` already worked.

## 0. One-time prerequisites

```bash
# Install flyctl if needed
curl -L https://fly.io/install.sh | sh

# Authenticate (opens a browser)
fly auth login

# Confirm you're in the right org
fly orgs list
```

## 1. Launch the app

From the **repo root** (so the Dockerfile path in `fly.toml` resolves):

```bash
fly launch \
  --copy-config \
  --no-deploy \
  --name cyntr-try \
  --region iad \
  --org personal
```

`--copy-config` tells `fly launch` to use the existing `deploy/fly.toml`
verbatim instead of regenerating one. `--no-deploy` skips the first build —
we want to create the volume and set secrets first.

If `fly launch` complains it can't find `fly.toml` in the root, pass it
explicitly:

```bash
fly launch --copy-config --config deploy/fly.toml --no-deploy --name cyntr-try
```

## 2. Create the persistent volume

Single-region for now (matches `fly.toml`):

```bash
fly volumes create cyntr_data \
  --region iad \
  --size 1 \
  --app cyntr-try
```

1 GB is enough for the beta. To resize later: `fly volumes extend <vol-id> --size 5`.

## 3. Set secrets

At minimum, one LLM provider key. **Never commit these** — `fly.toml`'s
`[env]` is for non-secrets only.

```bash
fly secrets set \
  ANTHROPIC_API_KEY=sk-ant-xxx \
  --app cyntr-try
```

Optional but recommended for private beta:

```bash
fly secrets set \
  CYNTR_API_KEY=cyntr_$(openssl rand -hex 16) \
  --app cyntr-try
```

If you want OTel traces / metrics shipped off-box:

```bash
fly secrets set \
  OTEL_EXPORTER_OTLP_ENDPOINT=https://api.honeycomb.io \
  OTEL_EXPORTER_OTLP_HEADERS="x-honeycomb-team=hcaik_xxx" \
  --app cyntr-try
```

## 4. Deploy

```bash
fly deploy --config deploy/fly.toml --app cyntr-try
```

First build is ~3-4 minutes (Go module download + binary build). Subsequent
deploys hit Fly's layer cache and finish in ~60s.

## 5. Verify

```bash
# Health endpoint (force-HTTPS will redirect HTTP → HTTPS):
curl -fSs https://cyntr-try.fly.dev/api/v1/system/health

# Tail logs while you test:
fly logs --app cyntr-try

# Open the dashboard in a browser:
fly open --app cyntr-try
```

Expected health response:

```json
{"status":"ok","modules":[...],"version":"hosted-beta",...}
```

## 6. Hook up the custom domain

```bash
# 1. Tell Fly we want to terminate TLS for try.cyntr.dev:
fly certs create try.cyntr.dev --app cyntr-try

# 2. Fly returns DNS instructions. Add them at your registrar:
#    A   try.cyntr.dev → <Fly IPv4 from `fly ips list`>
#    AAAA try.cyntr.dev → <Fly IPv6 from `fly ips list`>
#    Or use ACME DNS-01 challenge if you prefer (Fly tells you the CNAME).

# 3. Wait for issuance (usually <2 min):
fly certs show try.cyntr.dev --app cyntr-try

# 4. curl the new domain to confirm:
curl -fSs https://try.cyntr.dev/api/v1/system/health
```

## 7. Rollback (if a deploy goes sideways)

```bash
# List releases
fly releases --app cyntr-try

# Roll back to the previous one
fly deploy --image registry.fly.io/cyntr-try:<previous-tag> --app cyntr-try
```

`fly releases` shows each tag explicitly. The image registry is per-app.

## 8. Estimated monthly cost

For the default `shared-cpu-1x / 256 MB` machine on auto-stop:

| Component | $/mo (trickle traffic) | $/mo (always-on min=1) |
|---|---|---|
| Compute (shared-cpu-1x, 256MB) | ~$0-2 (auto-stopped) | ~$1.94 |
| Volume (1 GB) | $0.15 | $0.15 |
| Bandwidth (10 GB out) | ~$1 | ~$1 |
| TLS certs | free | free |
| **Total** | **~$2-5** | **~$5-7** |

Free Fly tier covers a non-trivial chunk of the above. Realistic budget
once `try.cyntr.dev` starts seeing 50-100 sessions/day: **~$5-15/mo**.

## 9. Common gotchas

- **Volume mount permission**: the Dockerfile pre-creates `/cyntr/data` owned
  by `cyntr:cyntr`. Fly preserves that on first mount. If you ever migrate to
  a new volume, run `fly ssh console` and `chown -R cyntr:cyntr /cyntr/data`
  before booting.
- **Cold start**: 2-4s on `shared-cpu-1x`. If unacceptable, set
  `min_machines_running = 1` in `fly.toml` (costs ~$2/mo extra).
- **OOM on 256 MB**: bump to 512 MB. Cyntr is small but Chromium (browser
  tool) and the OpenPolicyAgent embed are the heavy bits — both lazy-loaded,
  so memory grows under actual tool use.
- **Multi-region**: not supported with the current single-writer SQLite. If
  you want IAD + LHR, you need LiteFS or per-region tenant sharding. See
  the commented `[[regions]]` block at the bottom of `fly.toml`.
