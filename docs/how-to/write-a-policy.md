[Cyntr Docs](../README.md) > How-to > Write a policy

# Write a policy

At the end of this page you'll have a Rego policy that denies `shell_exec` to non-admin agents in any tenant whose name starts with `prod-`, tested locally with `cyntr policy test`, deployed via SIGHUP, and verified in the audit log.

Read [concepts/policy.md](../concepts/policy.md) first if you want to know why this works the way it does.

## 1. Write the rule

Create `policy.rego` next to your `cyntr.yaml`:

```rego
package cyntr.policy

default decision := "allow"

# Deny shell_exec to non-admin agents in production tenants.
decision := "deny" if {
  input.action == "tool_call"
  input.tool == "shell_exec"
  startswith(input.tenant, "prod-")
  input.agent != "admin"
}
```

The package name **must** be `cyntr.policy` and the rule **must** assign to `decision`. Cyntr queries `data.cyntr.policy.decision` — anything else is ignored.

## 2. Test it locally

`cyntr policy test` evaluates a single request against the loaded policy without restarting the server. Cyntr must be running for this to work — the test runs inside the live engine.

```bash
# Should deny
cyntr policy test --tenant prod-web --agent worker --action tool_call --tool shell_exec
# {"decision": "deny", "rule": "policy.rego:5"}

# Should allow (admin agent)
cyntr policy test --tenant prod-web --agent admin --action tool_call --tool shell_exec
# {"decision": "allow"}

# Should allow (non-prod tenant)
cyntr policy test --tenant dev-web --agent worker --action tool_call --tool shell_exec
# {"decision": "allow"}
```

If any of these come back wrong, fix the Rego and re-run. Iterate here, not in production.

## 3. Add a positive case (require_approval)

Extend the rule so `admin` in prod still needs human approval:

```rego
decision := "require_approval" if {
  input.action == "tool_call"
  input.tool == "shell_exec"
  startswith(input.tenant, "prod-")
  input.agent == "admin"
}
```

Re-test:

```bash
cyntr policy test --tenant prod-web --agent admin --action tool_call --tool shell_exec
# {"decision": "require_approval"}
```

## 4. Deploy without restart

```bash
kill -SIGHUP $(pgrep cyntr)
```

Cyntr recompiles the Rego in-place. Watch the log:

```
level=info msg="rego policy reloaded" files=1 took=12ms
```

If recompilation fails, you'll see:

```
level=error msg="rego reload failed; keeping previous policy" error="..."
```

Cyntr does **not** fail open. The previous compiled policy stays active.

## 5. Verify in the audit log

Trigger a real call from a non-admin agent in a prod tenant. Then:

```bash
cyntr audit query --tenant prod-web | jq '.[] | select(.action == "tool_call" and .tool == "shell_exec")'
```

You'll see an entry like:

```json
{
  "tenant": "prod-web",
  "agent": "worker",
  "action": "tool_call",
  "tool": "shell_exec",
  "decision": "deny",
  "rule": "data.cyntr.policy.decision",
  "hash_prev": "a1b2...",
  "hash": "c3d4..."
}
```

The `hash` field chains to `hash_prev` — tampering with any historical entry breaks the chain and the next `cyntr audit verify` run will catch it.

## 6. Wire it into CI

Treat the policy like code. A minimal GitHub Actions step:

```yaml
- name: Policy regression
  run: |
    ./cyntr start &
    sleep 2
    for case in tests/policy/*.json; do
      expected=$(jq -r .expected "$case")
      got=$(cyntr policy test \
        --tenant   "$(jq -r .tenant   "$case")" \
        --agent    "$(jq -r .agent    "$case")" \
        --tool     "$(jq -r .tool     "$case")" \
        --action   "$(jq -r .action   "$case")" \
        | jq -r .decision)
      if [ "$got" != "$expected" ]; then
        echo "$case: expected $expected, got $got"
        exit 1
      fi
    done
```

Pair this with a regression eval — see [run-evals.md](run-evals.md) — so prompt and policy changes are gated together.

## Common pitfalls

- **Forgot the `cyntr.policy` package name.** Your policy is loaded but the query returns nothing, and `default decision := "allow"` wins. Always grep for `package cyntr.policy` after editing.
- **YAML rule still matches.** Remember YAML rules run first. If a stale entry in `policy.yaml` is matching, the Rego never gets a chance. Delete or scope the YAML rule.
- **Args matching is exact.** Glob patterns are not supported on `args` in YAML. For pattern matching on args, use Rego.
- **Reloads in a clustered deploy.** SIGHUP only reloads one process. Send it to every replica, or restart them.

## Related

- [Concepts: Policy](../concepts/policy.md) — decision model, YAML vs Rego.
- [How-to: Run evals](run-evals.md) — gate policy changes with agent regression tests.
- [Reference: CLI — policy](../reference/cli.md#policy) — every flag for `cyntr policy`.
