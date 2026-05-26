[Cyntr Docs](../README.md) > Concepts > Skills

# Skills

A skill is a reusable bundle of {system-prompt fragment, tool grants, optional config} that an agent can activate on demand. Skills let you keep agents lean (small base prompt, small base tool list) and only load capabilities when they're needed.

## Anatomy of a skill

```yaml
# skills/incident-response.yaml
name: incident-response
description: Investigate production incidents using cloud, k8s, and logs.
prompt: |
  When investigating incidents, always start with the audit log and
  recent deployments. Read-only by default. Escalate before mutating.
tools:
  - kubectl
  - aws
  - knowledge_search
  - runbook_search
config:
  default_region: us-east-1
```

When an agent activates this skill mid-conversation (via the `skill_router` tool, the dashboard, or a config grant), the runtime:

1. Appends the skill's `prompt` to the system prompt.
2. Adds the skill's `tools` to the agent's effective tool list for the remainder of the session.
3. Exposes `config` as variables the prompt can reference.

## Where skills come from

| Source | How |
|--------|-----|
| **Built-in catalog** | `cyntr skill list` — curated first-party skills. |
| **OpenClaw imports** | `cyntr skill import-openclaw <path>` — converts OpenClaw skill files into Cyntr skills. |
| **GitHub search** | `cyntr skill search <query>` or dashboard search — community skills from public repos. |
| **Hand-authored** | Drop a YAML file in `skills/` and restart (or hot-reload via API). |

## On-demand loading

The `skill_router` tool, granted to most agents by default, lets the LLM decide *itself* which skill to load:

```
user > there's a 500 spike on api-prod
agent > [calls skill_router: load incident-response]
agent > pulling kubectl events and recent deploys...
```

This keeps the base system prompt short — adding 25 skills doesn't bloat every conversation, only the conversations that need them.

## Skills vs tools — when to use which

- **Tool** = a single capability (one function the agent can call).
- **Skill** = a *job* (a prompt fragment plus the right tools to do that job).

Add a tool when the agent needs a new ability. Add a skill when there's a recurring task pattern and you find yourself writing the same instructions into multiple agents.

## Related

- [How-to: Add a skill](../how-to/add-a-skill.md)
- [Concepts: Tools](tools.md)
- [Reference: Feature matrix](../reference/feature-matrix.md#skills)
