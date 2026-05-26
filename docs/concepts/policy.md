[Cyntr Docs](../README.md) > Concepts > Policy

# Policy

Policy is the layer between "the LLM said to do X" and "X happens." Every tool call, every chat turn, every federation inbound runs through it. This page explains the decision model, when to use YAML versus Rego, and how reloads work.

## The decision model

On every policy-relevant action, the runtime builds a `PolicyRequest`:

```go
type PolicyRequest struct {
    Tenant   string  // "prod-eng"
    Agent    string  // "incident-bot"
    User     string  // "alice@example.com" (or empty for system calls)
    Action   string  // "tool_call" | "chat" | "federation_inbound"
    Tool     string  // "shell_exec" (for tool_call)
    Args     map[string]any
}
```

The policy engine returns one of three decisions:

| Decision | Effect |
|----------|--------|
| `allow` | The action proceeds. Audited. |
| `deny` | The action is blocked. Audited. The agent is told the tool was denied; it may try a different approach. |
| `require_approval` | The action is held. A pending approval is created. A human approves or denies via dashboard, API, or Slack reaction. 5-minute default timeout. |

## YAML vs Rego — when to use which

Cyntr supports two policy backends. They run in series: YAML rules are evaluated first; if no YAML rule matches, the Rego policy (if loaded) decides; if neither matches, the default is `allow` (override per-tenant in config).

### YAML rules

Use YAML when your policy is a set of "(tenant, agent, tool) → decision" matches with priorities. This covers ~80% of real policies and is much easier to read in a code review.

```yaml
# policy.yaml
rules:
  - name: deny-shell-prod
    tenant: "prod-*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 20

  - name: allow-shell-cloudops-prod
    tenant: "prod-*"
    action: tool_call
    tool: shell_exec
    agent: "cloud-ops"
    decision: require_approval
    priority: 30   # higher priority wins

  - name: deny-aws-write
    tenant: "*"
    action: tool_call
    tool: "aws"
    args:
      command: "*delete*"
    decision: deny
    priority: 40
```

Rules support glob patterns on `tenant`, `agent`, `tool`, and exact match on `args` keys. The highest-priority matching rule wins.

### Rego (OPA)

Use Rego when your policy needs:

- **Logic.** `args.size > 100MB and (it's after-hours or the agent isn't on-call)`.
- **External lookups baked in.** "Allow only if the IAM role this caller is impersonating is in our managed list."
- **Composition across many resource fields.** YAML gets ugly past 3-4 conditions.

```rego
# policy.rego
package cyntr.policy

default decision := "allow"

# Deny shell_exec to non-admin agents in production tenants.
decision := "deny" if {
  input.action == "tool_call"
  input.tool == "shell_exec"
  startswith(input.tenant, "prod-")
  input.agent != "admin"
}

# Require approval for any AWS write action.
decision := "require_approval" if {
  input.action == "tool_call"
  startswith(input.tool, "aws")
  contains(input.args.command, "delete")
}
```

Cyntr embeds OPA as a library — there is no separate Rego sidecar. Place `policy.rego` (single file) or `policy.rego.d/` (directory of `.rego` files) in the working directory and Cyntr picks it up at start.

### Decision matrix

| Need | Use |
|------|-----|
| Tenant-scoped tool allowlists | YAML |
| Per-agent overrides | YAML |
| "Allow if X and (Y or Z)" | Rego |
| Conditional on args (size, regex, etc.) | Rego |
| Reviewable by non-engineers | YAML |
| Policy testing in CI | Either (`cyntr policy test`) |

## Worked example: deny shell_exec in prod tenants

Goal: in any tenant whose name starts with `prod-`, no agent except `admin` may call `shell_exec`. The `admin` agent's calls require human approval.

YAML (simplest if this is the whole policy):

```yaml
rules:
  - name: deny-shell-prod
    tenant: "prod-*"
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 10

  - name: approve-shell-admin-prod
    tenant: "prod-*"
    tool: shell_exec
    agent: admin
    decision: require_approval
    priority: 20    # overrides the deny above
```

Test it:

```bash
cyntr policy test --tenant prod-eng --agent web --action tool_call --tool shell_exec
# => deny

cyntr policy test --tenant prod-eng --agent admin --action tool_call --tool shell_exec
# => require_approval

cyntr policy test --tenant dev --agent web --action tool_call --tool shell_exec
# => allow
```

Step-by-step recipe with audit-log verification is in [how-to/write-a-policy.md](../how-to/write-a-policy.md).

## Reload semantics

- **YAML reload:** `kill -SIGHUP $(pgrep cyntr)` reloads `policy.yaml` without restarting the process. In-flight requests use the old policy; new requests use the new policy.
- **Rego reload:** Same SIGHUP triggers Rego recompile. If the new Rego fails to compile, Cyntr logs the error and **keeps the previous compiled policy** — no fail-open.
- **Federation policy sync:** If a peer pushes a policy version, that peer's outbound calls into your node are evaluated under the synced policy. Local agents still use your local policy. See [concepts/federation.md](federation.md).

## What gets audited

Every policy decision is written to the audit log with:

- The full `PolicyRequest`.
- The decision and the matching rule name (YAML) or Rego decision path.
- A SHA-256 hash chain to detect tampering.

Query it:

```bash
cyntr audit query --tenant prod-eng | jq '.[] | select(.decision == "deny")'
```

## Related reading

- [How-to: Write a policy](../how-to/write-a-policy.md) — step-by-step with CI integration.
- [Concepts: Multi-tenant](multi-tenant.md) — tenant scoping.
- [Concepts: Federation](federation.md) — cross-node policy enforcement.
- [Reference: CLI](../reference/cli.md#policy) — `cyntr policy test` flags.
