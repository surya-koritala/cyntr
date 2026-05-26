# User Model

Cyntr keeps a small, curated, per-(tenant, user) markdown document that is
loaded into every chat's system context. This is the "USER.md" you saw in
Hermes: a living biography the agent reads at the start of every
conversation and edits over time as it learns more about you.

There are two parts:

1. **`profile_md`** — the narrative. Background, interests, recent
   context. Updated by the agent (via the `user_model_write` tool) and by
   the auto-distiller (see below).
2. **`preferences_md`** — explicit, user-authored preferences. Format,
   tone, units, opt-outs. The agent reads these but rarely writes them.

Each section is capped at **4 KB**. That cap is deliberate — the profile
ships in every system prompt, so it has to stay terse. Anything you'd want
to remember in more detail belongs in the flat long-term memory store
(`modules/agent.MemoryStore`).

## How the agent reads and writes the user model

Two tools are exposed to every agent that wants them:

- `user_model_read` — returns the current `profile_md` and `preferences_md`.
- `user_model_write` — replaces one section with new content.
  `section` must be `profile` or `preferences`; oversize writes are
  rejected.

The tools dispatch over the IPC bus to the `usermodel` module
(`usermodel.get`, `usermodel.upsert_profile`, `usermodel.upsert_preferences`).

In addition to the explicit tool path, the runtime **injects** the profile
+ preferences into the system prompt automatically. You don't need any
agent config — if the `usermodel` module is registered, agents see a
"User profile:" + "User preferences:" block prepended to their system
prompt. If the module isn't registered, agents behave exactly as they did
before (no profile block, no error).

## Auto-distillation (T3.7)

Per-tenant opt-in. **Off by default.**

Once enabled, a daily background job pulls the user's recent chat activity
(short summaries of the last N exchanges, stored in `user_activity`),
hands them and the current `profile_md` to a Haiku-class LLM, and asks for
an updated profile. The result is written back via `UpsertProfile`. The
4 KB cap is enforced — oversized model responses are truncated at a line
boundary, never grown beyond the cap.

The distiller writes only the `profile_md` section. `preferences_md` is
never touched by automatic distillation — that's reserved for explicit
user/agent edits.

### Lifecycle

```
chat completes
  └─> agent_runtime publishes usermodel.record_activity
        └─> usermodel.Store.RecordActivity (one short row per chat)

once per day (default 04:00 UTC, via CYNTR_USERMODEL_DISTILL_CRON)
  └─> Ticker.loop matches cron expression
        └─> Distiller.Tick(ctx)
              └─> Store.ListActiveUsers (>=3 recent activity rows)
                    └─> Distiller.DistillUser(tenant, user) [bounded by Concurrency=5]
                          ├─> TenantDistillEnabled?    no → skip
                          ├─> user_opt_out?            yes → skip
                          ├─> rate-limited (<23h)?     yes → skip
                          ├─> >=3 activity rows?       no  → skip
                          ├─> provider.DistillChat(...)
                          ├─> validate, truncate to 4 KB
                          ├─> Store.UpsertProfile + MarkDistilled
                          └─> audit.Emit("usermodel.distill", ...)
```

### Configuration

| Setting | Default | Source |
| --- | --- | --- |
| Cron expression | `0 4 * * *` (04:00 UTC) | `CYNTR_USERMODEL_DISTILL_CRON` env |
| Distill model | `claude-haiku` | `CYNTR_USERMODEL_DISTILL_MODEL` env |
| Per-tenant opt-in | off | `Store.SetTenantDistillEnabled(tenant, true)` |
| Per-user opt-out | off (i.e. opted in) | `auto_distill: false` in `preferences_md` |
| Min sessions to distill | 3 | `usermodel.DefaultMinSessions` |
| Max sessions per prompt | 10 | `usermodel.DefaultMaxSessions` |
| Interval between distills (per user) | 23 h | `DistillerOptions.Interval` |
| Global concurrency | 5 | `DistillerOptions.Concurrency` |

### Privacy

Distillation is **opt-in at two levels**:

1. The **tenant** must explicitly call
   `Store.SetTenantDistillEnabled(tenant, true)`. No env var. No "enabled
   on all tenants" toggle. Operators have to opt their tenant in.
2. The **user** can opt out by adding a line to their `preferences_md`:
   ```
   - auto_distill: false
   ```
   Any of `false`, `no`, `off`, `0` are accepted. The check is run on
   every distill, so an opt-out takes effect immediately.

Every operation — success, error, skip — is logged to the audit module
under `action.type = "usermodel.distill"`. Query it with the standard
audit IPC:

```go
bus.Request(ctx, ipc.Message{
    Source: "you", Target: "audit", Topic: "audit.query",
    Payload: audit.QueryFilter{ActionType: "usermodel.distill", Tenant: "acme"},
})
```

Sensitive data is guarded in three places:

- Activity summaries are run through `MaskSecrets` + `RedactPII` before
  they're stored, so leaked API keys / SSNs / emails in a chat don't end
  up in the distiller's prompt.
- The distillation prompt explicitly tells the model to drop sensitive
  data and refuse to fabricate.
- Oversize / refusal-shaped responses are rejected, so the model can't
  silently blow past the 4 KB cap or write garbage that overwrites a
  good profile.

### Manual trigger

Operators (and the user themselves) can force an immediate distill:

- HTTP: `POST /api/v1/tenants/{tid}/users/{uid}/profile/distill` →
  returns `DistillResult`.
- IPC: send `usermodel.distill` to the `usermodel` module with
  `{tenant, user}`; returns `DistillResult`.

Manual triggers bypass the per-user rate limit, but they still honor the
tenant + user opt-in gates — there is no admin override for those.

### Cold-start nudge

When the agent runtime tries to load a user's profile and finds it empty,
it kicks off an asynchronous first-time distill. The chat itself does not
block. If the tenant hasn't opted in, the distiller no-ops on the trigger;
the agent continues with no injected profile, exactly as it would without
the user model installed.

## Worked example

After three sessions with the assistant on a Tuesday, Alice's profile is
empty. The chat path publishes `usermodel.record_activity` after each
exchange, so by Wednesday morning `user_activity` has three rows for
`acme/alice`. At 04:00 UTC, the ticker runs:

```
results := distiller.Tick(ctx)
```

For Alice, the distiller calls Haiku with:

```
You're maintaining a living profile of a user based on recent conversations.

Current profile (markdown):
---
(empty)
---

Recent conversations (summaries):
---
- [2026-05-25] user asked about Go module layout
- [2026-05-25] user followed up on slog setup
- [2026-05-25] user asked how to test cron schedulers
---

Update the profile to incorporate what you learned. Rules:
- Keep the markdown well-organized ...
```

Haiku returns:

```markdown
## Background
Go engineer. Recently set up structured logging with `slog` and is
ramping on testing patterns for time-based code.

## Recent context
- Working on a cron-scheduled background job; interested in deterministic
  tests over time-sensitive logic.
- Comfortable with `log/slog`, just adopted it.
```

The distiller validates the response, stores it via `UpsertProfile`,
stamps `last_distilled_at`, and emits an audit entry with `status =
"success"` and `detail = {old_size: 0, new_size: 187, sessions: 3, ...}`.

On Wednesday afternoon Alice chats again. The runtime loads the profile,
injects it into the system prompt, and the model now starts the
conversation with that context already in hand. On Thursday at 04:00 UTC
the cycle repeats — if Alice has new activity, the model is asked to
update the profile rather than rewrite it from scratch.

If Alice ever wants to stop the auto-updates, she edits her preferences:

```bash
curl -X PUT \
  /api/v1/tenants/acme/users/alice/profile \
  -d '{"preferences_md": "- prefers terse answers\n- auto_distill: false\n"}'
```

From the next tick onward, Alice's profile stops evolving automatically.
The existing profile stays in place; future explicit `user_model_write`
calls (from the agent) still work.
