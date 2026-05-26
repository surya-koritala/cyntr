[Cyntr Docs](../README.md) > Reference > Config

# `cyntr.yaml` reference

Full schema for the main config file. Loaded once at startup; re-read on SIGHUP for fields marked **hot-reloadable**.

A working minimal example lives at the repo root: [`cyntr.yaml.example`](../../cyntr.yaml.example). Copy it to `cyntr.yaml` and edit.

## Top-level structure

```yaml
version: "1"

listen:
  address: "127.0.0.1:8080"
  webui:   ":7700"

tenants:
  <tenant-id>:
    isolation: "namespace" | "process"
    cgroup:
      memory_limit: "1G"
      cpu_shares: 1024
    policy: "policy.yaml"
    quota:
      tokens_per_day: 1000000
      requests_per_min: 60

auth:
  provider: "" | "oidc"
  issuer:   ""
  client_id: ""

audit:
  storage_path: "audit.db"
  retention:    "90d"

federation:
  enabled: false
  peers: []

shell_exec_policies:
  - tenant:  "research"
    backend: "docker" | "inprocess"
    image:   "alpine:latest"
    timeout: "30s"

packs:
  loomfeed: false
```

## Fields

### `version` (string, required)

Schema version. Currently always `"1"`. Cyntr refuses to start if it doesn't recognize the value.

### `listen` (object, required)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `address` | string | `"127.0.0.1:8080"` | Internal API + proxy gateway. **Bind to `127.0.0.1` unless intentionally exposing.** |
| `webui` | string | `":7700"` | Dashboard + public REST API. Terminate TLS in front; do not expose plain HTTP to the public internet. |

### `tenants` (map, required)

Map of tenant ID → tenant config. The ID is the string used in all API paths (`/api/v1/tenants/<id>/...`) and policy rules.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `isolation` | string | `"namespace"` | `namespace` (logical separation, shared process) or `process` (separate child process per tenant). Process isolation costs ~30MB RAM per tenant. |
| `cgroup.memory_limit` | string | none | Linux cgroup v2 memory cap. Format: `"1G"`, `"512M"`. Ignored on non-Linux. |
| `cgroup.cpu_shares` | int | none | Relative CPU weight. 1024 = "one CPU's worth"; halve for half. |
| `policy` | string | `"policy.yaml"` | Path to the YAML policy file. Per-tenant overrides supported. |
| `quota.tokens_per_day` | int | unlimited | Rolling 24h token budget across all agents in this tenant. |
| `quota.requests_per_min` | int | unlimited | Per-minute request cap per tenant. |

Hot-reloadable: adding new tenants and changing `quota.*`. Changing `isolation` requires restart.

### `auth` (object, optional)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `provider` | string | `""` | `""` (API-key only) or `"oidc"` (OIDC + API key fallback). |
| `issuer` | string | `""` | OIDC issuer URL. Cyntr fetches `<issuer>/.well-known/openid-configuration`. |
| `client_id` | string | `""` | OIDC client ID for the dashboard. |

Additional OIDC settings live in env vars — see [reference/env-vars.md](env-vars.md).

### `audit` (object, optional)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `storage_path` | string | `"audit.db"` | SQLite file for the hash-chained audit log. |
| `retention` | duration | `"90d"` | How long to keep audit entries. Older entries are pruned daily. |

### `federation` (object, optional)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Master switch. When false, federation endpoints return 404. |
| `peers` | list | `[]` | Initial peer list. Peers can also be added at runtime via API. |

Each peer entry:

```yaml
peers:
  - name: "node-b"
    url: "https://node-b.example.com"
    api_key: "cyntr_..."
    allowed_inbound_agents: ["public-bot"]
```

See [concepts/federation.md](../concepts/federation.md).

### `shell_exec_policies` (list, optional)

Per-tenant sandboxing for the `shell_exec` tool. Tenants not listed get the default `inprocess` backend.

| Field | Type | Description |
|-------|------|-------------|
| `tenant` | string | Tenant ID (no wildcards). |
| `backend` | string | `"inprocess"` (host-direct, fast) or `"docker"` (container-sandboxed). |
| `image` | string | Docker image to launch (e.g. `"alpine:latest"`). Required when `backend: docker`. |
| `timeout` | duration | Per-command timeout. Default `30s`. |

The Docker backend runs each command with `--network none`, `--read-only`, `--tmpfs /tmp:rw,size=64m`, `--memory 256m`, `--cpus 0.5`. Stdout+stderr are merged and truncated to 64KB to match the in-process shape.

If `backend: docker` is requested but Docker is unavailable at startup, Cyntr logs a warning and falls back to `inprocess` — the process never crashes from a missing daemon.

### `packs` (object, optional)

Toggle optional packs (vertical capability bundles). Default off.

| Field | Type | Description |
|-------|------|-------------|
| `loomfeed` | bool | Registers RSS aggregation, content dedup, and Loomfeed posting tools. Also enabled by `CYNTR_PACK_LOOMFEED=1`. |

See [`packs/README.md`](../../packs/README.md).

## Hot-reload summary

`SIGHUP` reloads the following without restart:

- `tenants.*.quota.*`
- `tenants` additions (new tenants picked up)
- `policy.yaml` and `policy.rego`
- `audit.retention`
- `packs.*` (registers/unregisters pack tools)

Restart required for:

- `listen.*`
- `tenants.*.isolation`
- `tenants.*.cgroup.*`
- `auth.*`
- `federation.enabled`
- `shell_exec_policies.*` (containers spin up cleanly only on fresh start)

## Validation

```bash
cyntr doctor
```

Loads `cyntr.yaml` and reports schema errors. Use in CI to gate config changes:

```yaml
- name: Validate config
  run: ./cyntr doctor || exit 1
```

## Related

- [Reference: Env vars](env-vars.md) — secrets and runtime toggles.
- [Reference: CLI](cli.md) — every `cyntr` subcommand.
- [Concepts: Multi-tenant](../concepts/multi-tenant.md) — isolation modes, quota model.
- [Concepts: Policy](../concepts/policy.md) — how the `policy` field is used.
