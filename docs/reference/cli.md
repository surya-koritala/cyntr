[Cyntr Docs](../README.md) > Reference > CLI

# CLI reference

Every `cyntr` subcommand, with flags, environment variables, and exit codes.

The CLI is a thin client over the REST API for most subcommands, and an in-process orchestrator for `init`, `start`, `doctor`, `backup`, `restore`. `CYNTR_API_URL` overrides the API target (default `http://localhost:7700`). `CYNTR_API_KEY` sets the auth header.

## Top-level commands

| Command | What it does |
|---------|--------------|
| [`init`](#init) | Interactive 5-step setup wizard. |
| [`start`](#start) | Boot the server. |
| [`stop`](#stop) | Stop a running server (sends SIGTERM). |
| [`status`](#status) | Print server health. |
| [`doctor`](#doctor) | Validate config, check CLI auth, list module status. |
| [`chat`](#chat) | Quick REPL-style chat with an agent. |
| [`eval`](#eval) | Run an eval file. |
| [`docs`](#docs) | Open these docs in your browser. |
| [`backup`](#backup) | Snapshot all SQLite databases. |
| [`restore`](#restore) | Restore from a backup tarball. |
| [`version`](#version) | Print version. |

## Resource commands

| Command | Subcommands |
|---------|-------------|
| [`tenant`](#tenant) | `list` |
| [`agent`](#agent) | `create`, `list`, `chat` |
| [`audit`](#audit) | `query` |
| [`policy`](#policy) | `test` |
| [`skill`](#skill) | `list`, `install`, `import-openclaw`, `search`, `marketplace` |
| [`schedule`](#schedule) | `add`, `list`, `remove` |
| [`federation`](#federation) | `peers` |

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success. |
| `1` | Generic error (network, API, validation). |
| `2` | Usage error (bad flags). |
| `3` | Auth error (bad API key, denied by RBAC). |
| `4` | Policy denial (e.g. `cyntr eval` with a denied case). |

---

### init

```
cyntr init
```

5-step interactive wizard:

1. LLM provider — pick + paste API key.
2. Messaging channels — Slack/Teams/Discord/etc.
3. Cloud CLI access — AWS, Azure, GCP.
4. Security policy — preset (deny all / require approval / cloud-ops only / allow all).
5. First agent — name, model, prompt.

Writes `cyntr.yaml`, `policy.yaml`, `.env`, and a default tenant. Idempotent: re-running shows existing values as defaults.

### start

```
cyntr start [config-path]
```

Boots the server. Default config path is `cyntr.yaml` in the current directory. Reads `.env` automatically if present.

Environment:
- `CYNTR_LOG_LEVEL` — `debug` | `info` | `warn` | `error` (default `info`)
- All [env vars](env-vars.md) used by enabled modules.

Send `SIGHUP` to reload `cyntr.yaml`, `policy.yaml`, and `policy.rego` without restart.

### stop

```
cyntr stop
```

Sends SIGTERM to the local cyntr process. Use your orchestrator for remote deploys.

### status

```
cyntr status
```

GET `/api/v1/system/health`. Prints module statuses.

### doctor

```
cyntr doctor
```

Non-destructive sanity check:
- Config file loads cleanly.
- Policy file loads cleanly.
- At least one LLM provider is configured.
- Cloud CLIs (`aws`, `az`, `gcloud`, `kubectl`) are installed and authenticated (warn-only if missing).
- Required directories are writable.

Exits `0` even on warnings; non-zero only on fatal config errors.

### chat

```
cyntr chat <tenant> <agent>
```

Opens a stdin REPL. Each line is sent as a message; responses stream back. `Ctrl+D` exits.

### eval

```
cyntr eval <file.json> [flags]
```

Run an eval file. Flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | `text` or `junit` |
| `--repeat N` | `1` | Run each case N times; reports aggregate pass rate. |
| `--tenant` | from file | Override tenant. |
| `--out` | stdout | Write results to file. |

See [how-to/run-evals.md](../how-to/run-evals.md).

### docs

```
cyntr docs
```

Opens the bundled docs site in your default browser (served from `:7700/docs`).

### backup

```
cyntr backup [--out backup.tar.gz]
```

Snapshots `audit.db`, `sessions.db`, `memory.db`, `usage.db`, `knowledge_base.db`, `eval_runs.db`, `cyntr.yaml`, `policy.yaml`, `policy.rego` into a tarball. Safe to run while the server is up — SQLite is opened in read-only mode for the snapshot.

### restore

```
cyntr restore <backup.tar.gz>
```

Refuses to run if the server is up (would corrupt SQLite). Stop cyntr first.

### version

```
cyntr version
```

Prints `cyntr v1.1.0`. Exit `0`.

---

### tenant

```
cyntr tenant list
```

List all tenants. GET `/api/v1/tenants`.

### agent

```
cyntr agent create <tenant> <name> [--model <model>]
cyntr agent list <tenant>
cyntr agent chat <tenant> <agent> "<message>"
```

`--model` default is `mock` (useful for testing without a provider key).

### audit

```
cyntr audit query [--tenant <name>]
```

GET `/api/v1/audit`. Streams JSON lines. Pipe through `jq` to filter.

### policy

```
cyntr policy test [--tenant <t>] [--agent <a>] [--action <a>] [--tool <t>]
```

POST `/api/v1/policies/test`. Returns the policy decision for the synthetic request. Use in CI to gate policy changes — see [how-to/write-a-policy.md](../how-to/write-a-policy.md).

### skill

```
cyntr skill list
cyntr skill install <name>
cyntr skill import-openclaw <path>
cyntr skill search <query>
cyntr skill marketplace
```

`install` accepts either a name from the built-in catalog or a path to a local skill directory.

### schedule

```
cyntr schedule add <tenant> <agent> <interval> "<message>"
cyntr schedule list
cyntr schedule remove <id>
```

`interval` examples: `5m`, `1h`, `24h`. Result is delivered to the agent's default channel.

### federation

```
cyntr federation peers
```

GET `/api/v1/federation/peers`. Adding/removing peers is API-only — see [reference/api.md](api.md#federation).

## Related

- [Reference: API](api.md)
- [Reference: Config](config.md)
- [Reference: Env vars](env-vars.md)
- [How-to: Write a policy](../how-to/write-a-policy.md)
