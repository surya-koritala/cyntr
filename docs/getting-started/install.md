[Cyntr Docs](../README.md) > Getting Started > Install

# Install

You'll have a `cyntr` binary on disk and a server listening on `:7700` in under five minutes. Pick the path that matches your environment.

## Path 1 — Prebuilt binary (fastest)

```bash
curl -fsSL https://cyntr.dev/install.sh | bash
cyntr version
cyntr init        # 5-step wizard: provider, channel, security, policy, agent
cyntr start
```

Expected output of `cyntr version`:

```
cyntr v1.3.0
```

Expected output of `curl -s localhost:7700/api/v1/system/health`:

```json
{
  "data": {"status": "ok", "modules": [{"name": "agent", "status": "running"}, ...]},
  "meta": {"request_id": "...", "timestamp": "..."},
  "error": null
}
```

The install script writes the binary to `/usr/local/bin/cyntr`. To use a different location:

```bash
curl -fsSL https://cyntr.dev/install.sh | INSTALL_DIR=$HOME/.local/bin bash
```

## Path 2 — Docker

```bash
docker run --rm -p 7700:7700 \
  -e ANTHROPIC_API_KEY=$ANTHROPIC_API_KEY \
  -v $PWD/cyntr-data:/data \
  ghcr.io/surya-koritala/cyntr:latest
```

For a multi-container setup (Cyntr + Postgres + Slack adapter), use the included compose file:

```bash
git clone https://github.com/surya-koritala/cyntr.git
cd cyntr
docker compose up
```

Expected output:

```
cyntr-1  | level=info msg="config loaded" path=cyntr.yaml
cyntr-1  | level=info msg="server listening" addr=:7700
```

## Path 3 — From source

```bash
git clone https://github.com/surya-koritala/cyntr.git
cd cyntr
go build -o cyntr ./cmd/cyntr
./cyntr init
./cyntr start
```

Requirements: Go 1.26+, `git`, `sqlite3` (bundled via `modernc.org/sqlite`, no system package needed). Paths 1 and 2 require **no** prerequisites — only path 3 (from source) needs Go.

Verify the build:

```bash
go test ./... -count=1
```

Expected: 37 packages pass. If a package fails because an LLM API key is missing, that's expected — set `CYNTR_TEST_SKIP_LLM=1` to skip integration tests.

## Verify the install

Whichever path you used:

```bash
cyntr doctor
```

Expected output:

```
cyntr v1.3.0
[OK]  config: cyntr.yaml loaded
[OK]  policy: 3 rules loaded
[OK]  provider: anthropic (1 model)
[WARN] aws cli: not installed (optional — needed for cloud-ops agent)
```

`doctor` is a non-destructive sanity check. Re-run it any time something feels off.

## Next steps

- [Build your first agent](first-agent.md) — 5-minute walkthrough.
- [Production checklist](deploy.md) — before you put this on the public internet.
- [Concepts: Architecture](../concepts/architecture.md) — what just installed itself.
