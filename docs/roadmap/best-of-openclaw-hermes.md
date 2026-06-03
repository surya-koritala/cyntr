# Cyntr — "Best of OpenClaw + Hermes" Implementation Sprint Plan

> Goal: fold the strongest features from OpenClaw (🦞) and Hermes (☤) into Cyntr's
> Go, single-binary, multi-tenant, policy-gated platform — one ticket at a time,
> each independently buildable and testable.

## How to read this document

Every ticket has the same shape so you can pick one up cold:

- **ID / Name / Source** — where the idea comes from.
- **Goal** — the user-visible outcome.
- **Status in Cyntr today** — net-new, or extend existing code (several already have a seed).
- **Design** — how it fits Cyntr's kernel/module/IPC/tool/channel patterns.
- **Files** — concrete paths to create or modify.
- **Steps** — ordered implementation checklist.
- **Config** — new env vars / `cyntr.yaml` keys.
- **Depends on** — prerequisite tickets.
- **Tests** — unit (`*_test.go`), integration (`tests/integration/`), and manual verification.
- **Acceptance** — done criteria.
- **Effort** — S (≤1 day), M (2–4 days), L (1–2 weeks).

### Cyntr conventions every ticket must follow
- New subsystem = a **kernel module** implementing `kernel.Module` (`Name/Dependencies/Init/Start/Stop/Health`), registered in `cmd/cyntr/main.go`, exposing capabilities over the **IPC bus** with topic constants (`TopicX`).
- Durable state = a `store.go` over **modernc.org/sqlite** with schema in `migrations.go`. No new infra (no Redis/Postgres-only features; Postgres only behind the existing storage interface).
- Every new tool implements the `agent.Tool` interface and is registered in main.go.
- **Multi-tenant by default**: every store row and IPC payload carries `tenant` (+ `user` where relevant). Never add a global-only feature.
- **Policy + audit**: any new tool/action that touches the outside world must route through the policy engine and emit an audit record (same as existing tools). This is Cyntr's moat — do not bypass it.
- Tests live next to the code as `*_test.go`, table-driven, using the `mock` provider and in-memory/temp SQLite.

---

# Sprint 0 — Foundations (unblockers shared by later sprints)

These are small, high-leverage primitives that several features depend on. Do them first.

## F0.1 — Reflection/event hook on agent turn completion
**Source:** prerequisite for ☤ learning loop (A1–A4), G28.
**Goal:** a single, reliable place that fires after every agent turn with the full turn record (messages, tools used, outcome, tokens, tenant/user/session).
**Status:** net-new wiring; the agent runtime already has the data (`modules/agent/runtime.go`, `usage.go`).
**Design:** after a turn completes in `runtime.go`, publish an IPC **event** `agent.turn_completed` (fire-and-forget `bus.Subscribe` consumers). Payload = a `TurnRecord` struct. This decouples learning/memory/trajectory consumers from the hot path — same pattern as `usermodel`'s `record_activity`.
**Files:**
- `modules/agent/runtime.go` — emit event at end of turn.
- `modules/agent/types.go` — add `TurnRecord{Tenant,User,Session,Agent,Messages,ToolCalls,Outcome,Tokens,DurationMS,StartedAt}`.
- `kernel/ipc` topic const `agent.turn_completed`.
**Steps:** 1) define `TurnRecord`; 2) populate from existing turn loop; 3) publish via `bus` after the response is sent to the user (never block the user on subscribers); 4) add a no-op test subscriber.
**Tests:** unit — drive a turn with the mock provider, assert exactly one `agent.turn_completed` event with correct fields; assert the user response is returned even if a subscriber panics/errors.
**Acceptance:** event fires once per turn; user latency unchanged; tenant/user always populated.
**Effort:** S.

## F0.2 — Background job primitive (shared ticker + work queue)
**Source:** prerequisite for A1, A5, A6, G28/29.
**Goal:** reusable "do this work later, off the hot path, with retry and per-tenant rate limiting" so each learning feature doesn't reinvent the `usermodel` Ticker.
**Status:** extend — generalize `modules/usermodel/ticker.go` into a small shared package.
**Design:** add `kernel/jobs` (or `modules/agent/jobs`) with a `Queue` backed by a SQLite table (`jobs(id,tenant,kind,payload,run_after,attempts,state)`) plus a single ticker goroutine that leases due rows and dispatches to registered handlers. Respect `quota` module for per-tenant limits.
**Files:** `kernel/jobs/queue.go`, `queue_test.go`, `migrations`.
**Tests:** enqueue → lease → handler runs once; failure increments attempts and reschedules; per-tenant concurrency cap honored; survives restart (rows persist).
**Acceptance:** at-least-once execution, idempotent handlers documented, no busy-loop.
**Effort:** M.

---

# Sprint 1 — Learning & Memory loop  (☤ Hermes's real moat)

This sprint is the single highest-impact area: it's what Cyntr most lacks and what differentiates Hermes. All of it must be **per-tenant isolated and policy-gated** — a self-modifying agent in a multi-tenant platform is a security surface, so each generated skill/memory is tenant-scoped and passes the same capability checks as a hand-written skill.

## A5 — Cross-session search (FTS5 + LLM summarization)
**Source:** ☤. **Do this first — A1/A6 build on it.**
**Goal:** the agent can search its own past conversations and recall summaries across sessions.
**Status:** net-new module; sessions already persist (`modules/agent/store.go`).
**Design:** new `modules/recall` module. Create a SQLite **FTS5** virtual table over session messages (`sessions_fts(tenant,user,session,role,text)`). On `agent.turn_completed` (F0.1), index new messages. Provide IPC `recall.search(tenant,user,query)` → ranked snippets. Add a periodic job (F0.2) that, per session, produces an LLM summary (using the configured provider) stored in `session_summaries`. Expose a `recall_search` **tool** so the agent can call it mid-turn.
**Files:** `modules/recall/{module,store,indexer,summarizer,types}.go` + tests; `modules/agent/tools/recall_search.go`; register tool + module in main.go.
**Config:** `CYNTR_RECALL_SUMMARY_CRON` (default daily), `CYNTR_RECALL_MAX_SNIPPETS`.
**Depends on:** F0.1, F0.2.
**Tests:** unit — index 3 messages, FTS query returns the right one ranked first; tenant isolation (tenant A can't see tenant B); summarizer uses mock provider and writes a row. Integration — full turn → indexed → `recall_search` tool returns it next session.
**Acceptance:** sub-100ms search on 10k messages; strict tenant scoping; summaries regenerate incrementally.
**Effort:** M.

## A6 — Dialectic user modeling across sessions
**Source:** ☤ (Honcho-style). **Goal:** a deepening, queryable model of each user that improves over time.
**Status:** **extend** — `modules/usermodel` already has a `Distiller` + `Ticker` that distills a narrative profile. This ticket deepens it from a single narrative blob to a structured, evidence-backed model.
**Design:** extend `usermodel/store.go` with `user_facts(tenant,user,fact,confidence,source_session,updated_at)` and a "dialectic" pass in the `Distiller`: instead of overwrite, it proposes fact deltas (add/revise/retire) from recent activity + A5 summaries, with confidence. Keep the existing daily Ticker cadence. Expose facts to the agent via the existing `user_model_read` tool (extend its output) and let writes flow through `user_model_write` + the new fact table.
**Files:** `modules/usermodel/{store,distiller}.go` (extend), `facts.go` (new), tests; extend `modules/agent/tools/user_model_read.go`.
**Depends on:** A5 (summaries feed the dialectic pass).
**Tests:** distiller proposes a fact from a mock conversation; revision lowers/raises confidence; retired facts excluded from `user_model_read`; per-tenant isolation; manual-trigger path (`usermodel.distill`) still works.
**Acceptance:** facts accumulate and revise across ≥2 simulated sessions; no unbounded growth (retirement works); audit entry on each write.
**Effort:** M.

## A1 — Closed learning loop (self-reflection after tasks)
**Source:** ☤. **Goal:** after a complex task, the agent reflects on what worked/failed and produces durable improvements (memories, candidate skills, user facts).
**Design:** new `modules/learn` module subscribing to `agent.turn_completed` (F0.1). A complexity heuristic (≥N tool calls, or explicit `/reflect`, or error-then-recovery) enqueues a reflection job (F0.2). The job runs an LLM "after-action review" prompt over the `TurnRecord` and emits structured outputs: (a) a memory to `agent.memory` (`modules/agent/memory.go`), (b) a *candidate* skill → A2, (c) user-fact deltas → A6. Everything tenant-scoped and written through audit.
**Files:** `modules/learn/{module,reflector,heuristics,types}.go` + tests; register in main.go.
**Config:** `CYNTR_LEARN_ENABLED` (default off for safety), `CYNTR_LEARN_MIN_TOOLCALLS`.
**Depends on:** F0.1, F0.2, A2 (for skill candidates), A6 (for facts).
**Tests:** a scripted multi-tool turn triggers exactly one reflection; a trivial turn does not; outputs land in memory/skill-candidate tables; disabled flag fully suppresses it.
**Acceptance:** reflections are bounded (one per qualifying turn), produce at least a memory, never block the user, fully gated by the enable flag.
**Effort:** L.

## A2 — Autonomous skill creation from experience
**Source:** ☤. **Goal:** the agent proposes new reusable skills derived from successful task patterns.
**Status:** **extend** — the `skill` module already has catalog/registry/marketplace/loader/**compat (OpenClaw SKILL.md)** and the curator auto-prunes failing skills. Add the *creation* path.
**Design:** A1's reflector emits a `SkillCandidate` (name, description, SKILL.md body, declared `Capabilities`). New IPC `skill.propose` writes it to a **`skill_candidates` table in `pending` state — never auto-activated**. Activation requires either (a) operator approval via the existing Approvals dashboard page, or (b) a tenant policy that explicitly allows auto-activation of candidates with empty `shell`/`network` capabilities. Reuse the existing capability validation in `skill/types.go`.
**Files:** `modules/skill/{candidates,propose}.go` + tests; dashboard Approvals wiring; policy hook.
**Depends on:** A1; integrates with `policy` + Approvals (existing).
**Tests:** propose → lands in `pending`; approve → becomes `InstalledSkill` and is loadable by `skill_router`; a candidate declaring `shell:true` is blocked from auto-activation; capability validation rejects malformed manifests.
**Acceptance:** no skill ever self-activates with shell/network unless policy says so; approved candidates are indistinguishable from hand-written skills.
**Effort:** M.

## A3 — Self-improving skills during use
**Source:** ☤. **Goal:** skills get refined automatically based on success/failure telemetry.
**Status:** **extend** — curator already disables skills failing >7 days; this adds *improvement* not just pruning.
**Design:** record per-skill outcomes (success/fail/latency) keyed by skill+tenant in a `skill_stats` table, populated from `agent.turn_completed` when `skill_router` was used. When a skill's failure rate crosses a threshold, the curator enqueues an "improve" job: LLM rewrites the SKILL.md body given recent failure transcripts, producing a **new version** (reuse the existing agent Versions/rollback concept) submitted as an A2 candidate (approval-gated). Never mutate a live skill in place.
**Files:** `modules/skill/stats.go`, `modules/curator/improve.go` + tests.
**Depends on:** A2, F0.1, F0.2.
**Tests:** stats accumulate per tenant; threshold triggers one improve job; improved version is a candidate, not auto-live; rollback restores prior version.
**Acceptance:** improvement is versioned, approval-gated, and reversible.
**Effort:** M.

## A4 — Self-nudge to persist knowledge
**Source:** ☤. **Goal:** the agent periodically reminds itself to save durable knowledge it would otherwise lose at context compaction.
**Design:** lightweight — at context-compaction time (already a point in `runtime.go`) and at end of long sessions, inject a system nudge asking the model whether anything should be written to memory (`agent.memory`) or user facts (A6). Gate frequency per session to avoid nagging. This is prompt-level, not a new module.
**Files:** `modules/agent/runtime.go` (compaction hook), a `nudge.go` helper + test.
**Depends on:** A6.
**Tests:** nudge injected at compaction but at most once per N turns; nudge absent on short sessions; memory write path exercised with mock provider.
**Acceptance:** measurable increase in retained memories on long sessions, no user-visible spam.
**Effort:** S.

## A7 — Per-project context files
**Source:** ☤ (AGENTS.md) / 🦞 (AGENTS.md, SOUL.md, TOOLS.md). **Goal:** files that shape every conversation for a given workspace/agent.
**Design:** define per-agent/per-tenant context files (`CYNTR.md` or reuse `AGENTS.md`) loaded from the agent's workspace dir and prepended to the system prompt. Store path config per agent; cache and hot-reload via the existing `watcher` pattern in `skill/watcher.go`. Tenant-scoped paths only (no escaping the workspace root).
**Files:** `modules/agent/context_files.go` + test; config schema addition.
**Tests:** file content appears in the assembled prompt; edit + reload picked up; path traversal outside workspace rejected; missing file = no error.
**Acceptance:** context files injected deterministically, hot-reloaded, sandboxed to workspace.
**Effort:** S.

---

# Sprint 2 — Agent execution

## E22 — Isolated subagents for parallel workstreams
**Source:** ☤ / 🦞. **Goal:** spawn child agents that run in parallel, each with isolated session/context, results merged back.
**Status:** **extend** — `delegate` (single child) and `orchestrate` tools already exist (`modules/agent/tools/{delegate,orchestrate}.go`), and `federation/delegate.go` exists. This formalizes *parallel* fan-out with isolation.
**Design:** extend the orchestrate tool to launch N children concurrently (bounded worker pool, like the existing crew pipeline/parallel modes in `modules/crew`), each with its own session row and a tenant-scoped policy context inherited from the parent. Collect results, hand back to the parent turn. Enforce a max-fanout quota via the `quota` module.
**Files:** `modules/agent/tools/orchestrate.go` (extend), `modules/crew` reuse, tests.
**Tests:** 3 children run concurrently and all results returned; one child failing doesn't kill the batch; fanout cap enforced; each child's audit log is attributed to the parent trace ID.
**Acceptance:** deterministic merge, bounded concurrency, full audit trace.
**Effort:** M.

## E21 — Scripts-call-tools-via-RPC (zero-context batching)
**Source:** ☤. **Goal:** the agent writes a small script that calls multiple Cyntr tools in one turn, collapsing multi-step pipelines without burning context per step.
**Design:** extend the existing `code_interpreter` tool (`modules/agent/tools/code.go`) to expose a `cyntr.call_tool(name, args)` RPC surface inside the sandbox that round-trips over the IPC bus back to the `ToolRegistry` — **but every such call re-checks policy for the current tenant/agent** (the script must not be a policy bypass). Results returned to the script; only the final script output enters the model context.
**Files:** `modules/agent/tools/code.go` (extend), `rpc_bridge.go` + tests.
**Depends on:** policy module (mandatory).
**Tests:** script calls `file_read` then `http_request` and returns combined result in one turn; a tool denied by policy is denied inside the script too; sandbox cannot reach tools not granted to the agent; audit records each underlying tool call.
**Acceptance:** no policy escape via scripting; context cost is O(1) regardless of tool-call count; every sub-call audited.
**Effort:** L.

---

# Sprint 3 — Runtime & execution backends

## C15 — Per-session sandboxing
**Source:** 🦞. **Goal:** run untrusted (non-main / multi-tenant / channel-sourced) sessions inside a sandbox with a restricted tool allowlist.
**Status:** **extend** — `tenant/docker.go` + `tenant/process.go` and `shell_backend.go` already exist.
**Design:** add a per-agent `sandbox.mode` (`off|non-main|always`) and backend (`process|docker`, reusing `tenant/docker.go`). When active, shell/code tools execute inside the sandbox and the tool allowlist is intersected with a sandbox-safe set. This is the natural complement to the policy engine: policy decides *whether*, sandbox decides *where it runs*.
**Files:** `modules/agent/sandbox.go`, config schema, reuse `tenant/docker.go`; tests.
**Config:** `agents.<name>.sandbox.mode`, `.backend`.
**Tests:** non-main session runs shell in docker backend; main session runs on host; denied tools absent in sandbox; cleanup on session end.
**Acceptance:** untrusted sessions cannot touch host FS/network beyond the allowlist; mode honored per agent.
**Effort:** M.

## C14 — Pluggable execution backends (SSH, Singularity)
**Source:** ☤. **Goal:** run agent shell/code work on local, Docker, SSH, or Singularity backends behind one interface.
**Status:** **extend** — `shell_backend.go` already abstracts the backend.
**Design:** define a `ShellBackend` interface (likely already partially present) with `Run(ctx, cmd) (stdout,stderr,code)`; add `ssh` (golang.org/x/crypto/ssh) and `singularity` (wrap CLI) implementations. Selected per agent via config. Keep it a pure-Go interface so the single-binary story holds (SSH is pure Go; Singularity shells out to the host binary).
**Files:** `modules/agent/tools/shell_backend_ssh.go`, `_singularity.go` + tests.
**Depends on:** C15 (shares the backend abstraction).
**Tests:** SSH backend against a stub server runs a command and returns output; Singularity backend builds the right CLI invocation (assert command, mock exec); selection via config.
**Acceptance:** identical tool behavior across backends; failures surfaced cleanly.
**Effort:** M.

## C13 — Serverless hibernate backends (Modal, Daytona)
**Source:** ☤. **Goal:** agent execution environment hibernates when idle and wakes on demand — near-zero idle cost.
**Status:** net-new backends on top of C14's interface.
**Design:** implement `ShellBackend` for **Modal** and **Daytona** via their HTTP APIs: provision/wake a sandbox on first use, run, and let it idle-hibernate. Cyntr itself stays a single always-on binary (cheap); only heavy *execution* is offloaded. Cache sandbox handles per (tenant,agent) with TTL.
**Files:** `modules/agent/tools/shell_backend_modal.go`, `_daytona.go` + tests.
**Config:** `MODAL_TOKEN`, `DAYTONA_API_KEY`, per-agent backend selection.
**Depends on:** C14.
**Tests:** backend issues correct provision/run/teardown calls against a mocked HTTP API; cold-start path wakes then runs; idle handle expires.
**Acceptance:** runs succeed cold and warm; no leaked sandboxes; cost-relevant idle teardown verified via mock.
**Effort:** L.

## C16 — Native Windows + Termux/Android support
**Source:** ☤. **Goal:** the single binary runs cleanly on native Windows and Termux/Android.
**Status:** **mostly free** — Go cross-compiles and modernc.org/sqlite is pure Go (no cgo). This ticket is about the rough edges.
**Design:** audit for POSIX-only assumptions (paths, signals, `os/exec` shells, file locks); guard with `runtime.GOOS`; ship `GOOS=windows` and `GOOS=android` build targets in CI; provide a PowerShell install snippet. Default shell tool picks `cmd`/`powershell` on Windows, `sh` on Termux.
**Files:** `cmd/cyntr/*` platform guards, `scripts/install.ps1`, CI matrix, `docs/getting-started/install.md`.
**Tests:** CI builds the three targets; a path-handling unit test runs on Windows runner; shell tool selects the right shell per GOOS.
**Acceptance:** `go build` green on windows/linux/android; smoke test (`cyntr init && cyntr start`) passes on Windows runner.
**Effort:** M.

---

# Sprint 4 — Models

## D17 — Broad provider coverage + frictionless switch
**Source:** ☤. **Goal:** support 200–300+ models with one-command/one-config switch and no code change.
**Status:** **extend** — 8 providers + OpenRouter (100+) already exist; OpenRouter alone covers most of the breadth.
**Design:** (a) make model selection a first-class config/CLI op (`cyntr model use <provider>:<model>`) writing to config, hot-applied; (b) ensure OpenRouter passthrough accepts any model id; (c) add a generic **OpenAI-compatible** provider (base-URL + key) so NovitaAI, z.ai/GLM, Kimi/Moonshot, MiniMax, NVIDIA NIM, etc. work without per-vendor code. Most of the "300+" comes free from OpenRouter + the generic shim.
**Files:** `modules/agent/providers/openai_compatible.go` + test; `cmd/cyntr` model subcommand; config schema.
**Tests:** generic provider talks to a stub OpenAI-compatible server; `cyntr model use` updates config and the runtime picks it up without restart; unknown model id surfaces a clean error.
**Acceptance:** any OpenAI-compatible endpoint usable via config only; model switch needs no rebuild.
**Effort:** M.

## D18 — Model failover & auth-profile rotation
**Source:** 🦞. **Goal:** automatic fallback across providers/keys on error or rate-limit.
**Design:** wrap provider selection in `runtime.go` with an ordered **failover chain** (config: `models.fallbacks: [anthropic:claude, openrouter:..., ollama:...]`). On 429/5xx/timeout, advance to the next; rotate among multiple keys per provider (auth profiles) before failing. Emit metrics (`observability` module) per provider for health.
**Files:** `modules/agent/failover.go` + tests; config schema; metrics wiring.
**Tests:** primary returns 429 → secondary used; all fail → clean aggregate error; key rotation cycles through profiles; metrics incremented.
**Acceptance:** transparent failover, bounded retries, observable per-provider health.
**Effort:** M.

## D20 — OAuth subscription login (sign in with provider)
**Source:** 🦞 / ☤ (Nous Portal). **Goal:** authenticate to a model provider via OAuth instead of pasting API keys.
**Design:** add an OAuth device/auth-code flow for providers that support it (e.g. OpenAI/ChatGPT, a "Nous Portal"-style aggregator). Tokens stored per-tenant in the existing secrets store, refreshed automatically. Surfaced in the onboarding wizard (F25). **Reuse the existing OIDC code** in `auth/oidc.go` for the OAuth plumbing.
**Files:** `modules/agent/providers/oauth.go`, secrets storage, wizard step; tests.
**Depends on:** F25 (wizard) for UX; `auth/oidc.go` reuse.
**Tests:** mocked OAuth flow yields a token; expired token auto-refreshes; token stored encrypted per tenant; revoke path clears it.
**Acceptance:** a user can connect a provider without an API key; tokens scoped per tenant; refresh works.
**Effort:** M.

## D19 — Bundled Tool Gateway (search / image / TTS / browser under one key)
**Source:** ☤ (Nous Portal Tool Gateway). **Goal:** one configured gateway provides web search, image gen, TTS, and cloud browser so users don't collect 5 keys.
**Status:** **extend** — `websearch`, `imagegen`, `transcribe`, `chromium`/`advanced_browser` tools already exist; this routes them through one optional gateway.
**Design:** add a `toolgateway` config block (base-url + key). When set, the existing tools route their HTTP calls through the gateway instead of per-vendor endpoints; when unset, current per-vendor behavior is unchanged (per-tool override always wins). Pure routing layer.
**Files:** `modules/agent/tools/gateway.go` (shared client), minimal edits to the four tools; tests.
**Tests:** with gateway set, each tool calls the gateway URL; with a per-tool key set, that overrides the gateway; gateway off = unchanged behavior.
**Acceptance:** one key lights up all four capabilities; per-tool overrides respected; no regression when unused.
**Effort:** M.

---

# Sprint 5 — Channels & presence  (mostly 🦞)

## B12 — DM pairing for untrusted senders
**Source:** 🦞 / ☤. **Goal:** unknown senders on a channel get a pairing code and are not processed until approved.
**Status:** net-new policy on top of the existing `channel` module + `auth` allowlist.
**Design:** add a `dmPolicy` (`pairing|open|closed`) per channel adapter. On inbound from an unknown (tenant,channel,userID), the `channel/manager.go` issues a short code, stores `pending_pairings`, and replies with it instead of invoking the agent. `cyntr pairing approve <channel> <code>` (CLI + Approvals dashboard) adds the sender to a tenant-scoped allowlist. Integrates with RBAC.
**Files:** `modules/channel/pairing.go`, `manager.go` (gate inbound), CLI cmd, store + tests.
**Config:** `channels.<name>.dmPolicy`, `.allowFrom`.
**Tests:** unknown sender gets a code and agent is NOT invoked; approve → next message reaches the agent; `open` + `"*"` allowlist processes everyone; per-tenant isolation of allowlists.
**Acceptance:** default `pairing` blocks unknown inbound; approval is auditable; works across all adapters via the manager (not per-adapter code).
**Effort:** M.

## B11 — Expanded channels (iMessage, IRC, LINE, Nostr, etc.)
**Source:** 🦞 (breadth). **Goal:** add high-value channels beyond the current 12.
**Status:** **extend** — `ChannelAdapter` interface is clean; 12 adapters already exist. Pure adapter additions.
**Design:** implement `ChannelAdapter` per new platform (`Name/Start/Stop/Send`). Prioritize by value/effort: **IRC** (pure Go, easy), **LINE** (HTTP webhook, easy), **Nostr** (libs exist), **Matrix** already present. iMessage is macOS-host-only — document as a companion-app feature (ties to B10), not a server adapter.
**Files:** `modules/channel/{irc,line,nostr}/adapter.go` + tests; register in main.go behind config.
**Tests:** per adapter — inbound message parsed to `InboundMessage` with tenant/agent resolved; `Send` formats correctly (use mock transport); start/stop clean.
**Acceptance:** each new adapter passes the same manager-level conformance test the existing 12 do; enabled only when configured.
**Effort:** M per adapter (do as a checklist).

## B8 — Voice: wake word + talk mode + TTS
**Source:** 🦞 / ☤. **Goal:** speak to and hear the agent.
**Status:** **partial** — `transcribe` (Whisper, STT) tool exists; TTS and wake word are new. Server-side this is mostly STT→agent→TTS plumbing; wake word lives in a client app (B10).
**Design (server side):** add a `voice` surface to the web/API layer: accept an audio blob → `transcribe` → agent turn → **TTS** (new tool wrapping ElevenLabs/OpenAI TTS, routable via D19 gateway) → return audio. Wake-word/continuous capture is a client concern handled in the companion app (B10) or browser dashboard.
**Files:** `modules/agent/tools/tts.go` + test; `web/api/voice.go` + test.
**Depends on:** D19 (optional gateway), B10 (client capture) for full UX.
**Tests:** TTS tool calls provider with text, returns audio bytes (mock); voice endpoint does STT→turn→TTS round-trip with mock provider.
**Acceptance:** an audio request returns spoken audio; provider selectable; works through gateway or direct key.
**Effort:** M (server) — full hands-free UX gated on B10.

## B9 — Live agent-driven canvas (A2UI)
**Source:** 🦞. **Goal:** the agent renders/updates a live visual workspace the user can see and control.
**Status:** net-new; Cyntr has a web dashboard (`web/static`) to build on.
**Design:** define a small **A2UI-style** declarative schema (JSON describing panels/components) the agent can emit via a `canvas_render` tool; the dashboard opens a WebSocket and live-renders updates. State stored per session, tenant-scoped. Start minimal (text/markdown/table/image/button components) and expand.
**Files:** `modules/agent/tools/canvas.go`, `web/api/canvas_ws.go`, `web/static/canvas.*` + tests.
**Tests:** tool emits a canvas doc → persisted + broadcast to subscribers; component schema validated; tenant isolation on the WS channel.
**Acceptance:** agent can draw and update a canvas live in the dashboard; reconnect restores state.
**Effort:** L.

## B10 — Companion apps (macOS / iOS / Android nodes)
**Source:** 🦞. **Goal:** native clients that pair to the gateway for voice capture, canvas, camera, screen.
**Status:** net-new and **out of the Go single-binary scope** — these are separate app projects.
**Design:** define a **node pairing protocol** (WebSocket + device pairing code, reuse B12 pairing semantics) and a thin client SDK. The macOS menu-bar app, iOS, and Android nodes are separate repos consuming that protocol. **Recommendation: scope this sprint to the *protocol + reference web client* only**; defer native apps as follow-on projects with their own roadmap, since they don't fit the Go platform and carry app-store/signing overhead.
**Files:** `modules/node/{protocol,pairing}.go` + tests; reference client in `web/static`.
**Depends on:** B12 (pairing), B9 (canvas), B8 (voice).
**Tests:** node pairs via code; WS auth enforced; capability negotiation (voice/canvas/camera) works against a stub client.
**Acceptance:** a browser/reference node can pair and exchange voice/canvas; native apps explicitly deferred and documented as such.
**Effort:** L (protocol) + native apps = separate efforts (flag to user, don't bundle).

---

# Sprint 6 — UX & operations

## F24 — Switchable personalities
**Source:** ☤. **Goal:** named personas selectable per conversation (`/personality <name>`).
**Design:** personalities = named system-prompt fragments stored per tenant (`personalities` table) and selectable via a chat command + per-session state. Compose with A7 context files. Ship a few defaults.
**Files:** `modules/agent/personality.go`, command handling in runtime/channels, store + tests.
**Tests:** selecting a personality changes the assembled system prompt; persists for the session; per-tenant catalog; unknown name errors cleanly.
**Acceptance:** personality switch is immediate, session-scoped, tenant-isolated.
**Effort:** S.

## F23 — Rich TUI
**Source:** ☤. **Goal:** a real terminal UI (multiline editing, slash-command autocomplete, history, interrupt-and-redirect, streaming tool output).
**Status:** net-new; Cyntr has a CLI but not a full TUI.
**Design:** add a `cyntr tui` subcommand using a Go TUI lib (Bubble Tea). It talks to the local gateway over the existing REST/IPC, streams responses, autocompletes slash commands from the registered tool/skill list, supports Ctrl-C interrupt-and-redirect. Pure client; no server changes.
**Files:** `cmd/cyntr/tui/*.go` + tests; add `github.com/charmbracelet/bubbletea` dep.
**Tests:** model-update unit tests (input → state transitions); autocomplete returns expected commands; streaming renders incrementally (golden tests on the view).
**Acceptance:** `cyntr tui` gives a usable streaming chat with autocomplete and interrupt.
**Effort:** L.

## F26 — `doctor` diagnostics command
**Source:** 🦞 / ☤. **Goal:** `cyntr doctor` surfaces misconfig and risky settings.
**Design:** a subcommand that runs checks: provider keys present/valid, channel tokens, DB writable, policy files parse, risky DM policies (`open` + `"*"`), OIDC config, port conflicts, sandbox backend availability. Output a clear pass/warn/fail report. Each check is a small `Check` interface so it's easy to extend.
**Files:** `cmd/cyntr/doctor/*.go` + tests.
**Tests:** each check has a unit test for pass/warn/fail; aggregate exit code reflects worst severity; redacts secrets in output.
**Acceptance:** `cyntr doctor` catches the common misconfigurations and never prints secrets.
**Effort:** M.

## F25 — Guided onboarding wizard
**Source:** 🦞 / ☤. **Goal:** one guided flow to configure provider, channels, security, first agent.
**Status:** **extend** — README says `cyntr init` already runs a wizard; this deepens it.
**Design:** extend `cyntr init` into a step-by-step flow (reuse F23 TUI primitives): pick provider (incl. D20 OAuth), set channels (with B12 DM policy defaults), create first agent, run F26 doctor at the end. Writes `cyntr.yaml`. Idempotent and re-runnable.
**Files:** `cmd/cyntr/init/*.go` (extend) + tests.
**Depends on:** F26, D20, B12 (for the relevant steps).
**Tests:** scripted answers produce a valid config; re-run is safe; doctor invoked at the end; bad input re-prompts.
**Acceptance:** a fresh user reaches a working first agent in one flow; final config passes `doctor`.
**Effort:** M.

## F27 — One-line installer + migrate-from-X importer
**Source:** ☤ (`hermes claw migrate`). **Goal:** `curl | bash` install, plus import config/memories/skills from OpenClaw (and Hermes).
**Status:** **partial** — `skill/compat` already loads OpenClaw SKILL.md.
**Design:** (a) an `install.sh`/`install.ps1` that downloads the right release binary and runs `cyntr init`; (b) `cyntr migrate openclaw [--dry-run]` that reads `~/.openclaw` (config, allowlists, skills) and maps it into Cyntr's config + skill registry (reusing the existing compat loader); a `--dry-run` preview and conflict handling.
**Files:** `scripts/install.sh`, `install.ps1`; `cmd/cyntr/migrate/openclaw.go` + tests.
**Tests:** migrate parses a fixture `~/.openclaw` dir; dry-run mutates nothing; skills import via compat loader; conflicts reported, not silently overwritten.
**Acceptance:** one command installs; one command imports an OpenClaw setup with a safe preview.
**Effort:** M.

---

# Sprint 7 — Research tooling  (☤)

## G28 — Batch trajectory generation
**Source:** ☤. **Goal:** generate agent run trajectories at scale for evaluation/training datasets.
**Status:** **extend** — `evals` module + `/evals` already run agents over cases; this adds trajectory capture/export.
**Design:** on `agent.turn_completed` (F0.1), optionally persist full **trajectories** (prompt, tool calls, observations, outputs) to a `trajectories` table when a run is tagged `record`. A `cyntr trajectory run --suite X --n N` command fans out batch runs (reuse E22 parallelism + the jobs queue) and exports JSONL. Tenant-scoped; opt-in only (privacy).
**Files:** `modules/eval/trajectory.go`, `cmd/cyntr/trajectory/*.go`, export writer + tests.
**Depends on:** F0.1, F0.2, E22.
**Tests:** a recorded run produces a complete trajectory row; batch of N produces N JSONL records; recording off by default; tenant isolation.
**Acceptance:** reproducible JSONL trajectory export; opt-in; scales via the job queue.
**Effort:** M.

## G29 — Trajectory compression for training
**Source:** ☤. **Goal:** compress recorded trajectories into a compact, model-training-friendly form.
**Design:** a `cyntr trajectory compress` step that dedupes/normalizes tool I/O, strips secrets (reuse the existing PII redaction), and emits a canonical compact schema. Pure offline transform over G28 output.
**Files:** `modules/eval/compress.go` + tests.
**Depends on:** G28.
**Tests:** compression reduces size while preserving the decision sequence; secrets/PII removed; round-trips to a documented schema.
**Acceptance:** compressed output is smaller, PII-free, and schema-valid.
**Effort:** S.

---

# Suggested execution order (dependency-respecting)

1. **Sprint 0** (F0.1, F0.2) — unblocks almost everything.
2. **Sprint 1** A5 → A6 → A2 → A1 → A3 → A4 → A7 — the differentiator; do it early.
3. **Sprint 2** E22 → E21 — needed by research tooling and improves everything.
4. **Sprint 4** D17 → D18 → D19 → D20 — cheap wins, mostly extensions.
5. **Sprint 3** C15 → C14 → C13 → C16 — execution/runtime depth.
6. **Sprint 5** B12 → B11 → B8 → B9 → B10 — presence; B10 native apps deferred.
7. **Sprint 6** F26 → F24 → F25 → F27 → F23 — ops/UX polish.
8. **Sprint 7** G28 → G29 — research, last.

# Cross-cutting "definition of done" for every ticket
- [ ] Multi-tenant: every row/payload carries `tenant`; cross-tenant access test passes.
- [ ] Policy + audit: outward actions gated and audit-logged.
- [ ] Tests: unit (`*_test.go`, mock provider, temp SQLite) + at least one `tests/integration` path + a documented manual check.
- [ ] Config documented in `docs/reference/feature-matrix.md` and env-var reference.
- [ ] No new always-on infra; SQLite or behind the storage interface.
- [ ] `cyntr doctor` (once F26 lands) gains a check if the feature can be misconfigured.
