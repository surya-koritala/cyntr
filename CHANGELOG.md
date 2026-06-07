# Changelog

All notable changes to Cyntr are documented here. Versions follow [SemVer](https://semver.org).
Releases are published at https://github.com/surya-koritala/cyntr/releases with prebuilt
single-binary assets (built by `.github/workflows/release.yml` on each `v*` tag).

## [Unreleased]

## [1.3.0] — 2026-06-06

**Theme: security hardening + native subagent coordination + a working install path.**

### Security
- Completed the June 2026 full-repo security audit backlog (13 CRITICAL, 51 HIGH,
  42 MEDIUM, 31 LOW findings) across three waves (#66, #67, #68).
- New `kernel/netguard` package: one hardened SSRF guard (public-IP-only validation +
  redirect-revalidating HTTP client) shared by the skill marketplace, workflow webhook
  steps, federation peers, MCP HTTP transport, the proxy, auth JWKS, and the web API.
- Channel adapters now **fail closed** with signature/secret verification: Slack
  (`X-Slack-Signature`), Discord (Ed25519), Telegram secret-token, WhatsApp
  (`X-Hub-Signature-256`), Twilio (`X-Twilio-Signature`), Mattermost command token,
  Google Chat (RS256 bearer JWT), and the generic webhook (HMAC-SHA256).
- Hard tenant isolation across agent memory, knowledge FTS, curator, SLA, proxy,
  federation queries, and MCP clients (now keyed by `(tenant, name)`).
- AuthZ: JWT signatures verified (no more bypass), OIDC ID-token signature + `aud`
  enforced, `audit.query`/policy-approval handlers require authorized callers, curator
  admin via real scope, `/auth/me` rejects unknown credentials, API keys default to
  least privilege, MCP server is token-gated, and the skill sandbox enforces capabilities.
- Concurrency fixes (race-clean under `-race`): IPC bus panic-recovery and send/close
  races, logger level, jobs queue, eval/crew/workflow/scheduler shared state.

### Added
- **#48 — native stateful subagent coordination**: a tenant-scoped, policy-gated
  shared-context channel built on the existing recall store + IPC bus (no external
  shared-memory dependency).
- **Release pipeline** (`.github/workflows/release.yml`): cross-compiles binaries for
  linux/darwin/windows (amd64+arm64) and uploads them with SHA-256 checksums on tag push.

### Changed
- **Install is now a real one-liner.** `install.sh` / `scripts/install.ps1` download the
  prebuilt single binary (no Go required), verify the checksum, surface real errors, and
  support `INSTALL_DIR` / `CYNTR_VERSION` overrides — replacing the old script that pinned
  `v0.3.0`, required Go, built from source, and silenced all errors.
- Version is injected into release binaries via ldflags, so `cyntr version` always matches
  the published tag.

### Fixed
- `curl -fsSL https://cyntr.dev/install.sh | sh` no longer installs an ancient `v0.3.0`
  build (or fails silently). Version strings across code, README, and docs corrected from
  the stale `1.1.0` to `1.3.0`.

> **Publishing note:** the website at cyntr.dev serves a copy of `install.sh` — redeploy
> the site after this release so the public one-liner picks up the new script.

## [1.2.0] — 2026-05-27 (draft)
Platform upgrade: `tool_plan`, OPA/Rego policy, federation demo, Curator v1, Cloud Ops vertical.
(Drafted but never published; superseded by 1.3.0.)

## [1.1.0] — 2026-03-26
Webhook integrations, SLA monitoring, observability.

## [1.0.0] — 2026-03-24
General availability: kernel, multi-tenant agents, policy + audit, 9 channels, dashboard.

[Unreleased]: https://github.com/surya-koritala/cyntr/compare/v1.3.0...HEAD
[1.3.0]: https://github.com/surya-koritala/cyntr/releases/tag/v1.3.0
[1.2.0]: https://github.com/surya-koritala/cyntr/releases/tag/v1.2.0
[1.1.0]: https://github.com/surya-koritala/cyntr/releases/tag/v1.1.0
[1.0.0]: https://github.com/surya-koritala/cyntr/releases/tag/v1.0.0
