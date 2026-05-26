[Cyntr Docs](../README.md) > How-to > Add a skill

# Add a skill

A skill is a YAML file in `skills/` that bundles a system-prompt fragment, a tool grant list, and optional config. Agents activate skills on demand.

## Schema

```yaml
# skills/<name>.yaml
name: incident-response
description: Investigate production incidents using cloud, k8s, and logs.
prompt: |
  When investigating incidents, always start with the audit log and
  recent deployments. Read-only by default; escalate before mutating.
tools:
  - kubectl
  - aws
  - knowledge_search
  - runbook_search
config:
  default_region: us-east-1
```

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Unique identifier (kebab-case). |
| `description` | yes | One-line summary; shown in the marketplace and `skill_router`. |
| `prompt` | yes | Appended to the agent's system prompt when active. |
| `tools` | no | Tools added to the agent's effective grant list when active. |
| `config` | no | Key/value pairs available as template variables in `prompt`. |

## Activation

Three ways:

1. **Static grant** — list the skill in the agent's `skills:` field at creation time. Always active.
2. **`skill_router` tool** — granted to most agents; the LLM picks a skill from the catalog mid-conversation.
3. **Dashboard / API** — `POST /api/v1/tenants/<t>/agents/<a>/skills/<skill>/activate` for the session.

## Marketplace

```bash
cyntr skill list                            # built-in catalog
cyntr skill search "cost"                   # GitHub community skills
cyntr skill install incident-response       # install from catalog
cyntr skill import-openclaw ./path/to/skill # OpenClaw → Cyntr conversion
```

Installed skills land in `skills/` so they're git-trackable.

## Related

- [Concepts: Skills](../concepts/skills.md)
- [Concepts: Tools](../concepts/tools.md)
- [Reference: Feature matrix](../reference/feature-matrix.md#skills)
