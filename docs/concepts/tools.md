[Cyntr Docs](../README.md) > Concepts > Tools

# Tools

A tool is a callable capability an agent can invoke during a chat turn. Cyntr ships ~28 built-in tools, lets you add custom tools as YAML (no code) or as Go modules, and bridges to ~8 external MCP servers.

## Three sources

1. **Built-in tools** — registered at startup by `cmd/cyntr/main.go`. Examples: `shell_exec`, `web_search`, `aws`, `kubectl`, `database_query`, `knowledge_search`.
2. **YAML tools** — files in `tools/*.yaml` are auto-loaded at startup. Define parameters, a command template, and a timeout. See [how-to/add-a-tool.md](../how-to/add-a-tool.md).
3. **MCP servers** — connect to Model Context Protocol servers and import their tool surface. Configured via `cyntr.yaml` or the dashboard.

## How a tool call works

1. LLM emits a tool call: `{name: "aws", args: {command: "ecs list-clusters"}}`.
2. Runtime checks the agent's tool grant list. If the tool isn't on it, the call fails fast.
3. Runtime builds a `PolicyRequest` and asks the policy engine. `allow` → execute; `deny` → return denial text to the LLM; `require_approval` → block until a human resolves.
4. Tool executes. Results (typed) are appended to the chat history.
5. Audit log captures everything.

## Categories

| Category | Examples |
|----------|----------|
| **System** | `shell_exec`, `code_interpreter` |
| **Files** | `file_read`, `file_write`, `file_search` |
| **Web** | `browse_web`, `chromium_browser`, `web_search`, `http_request` |
| **Data** | `database_query`, `pdf_reader`, `knowledge_search`, `json_query`, `csv_query` |
| **Cloud** | `aws`, `aws_cross_account`, `aws_cost_explorer`, `kubectl` |
| **Integrations** | `github`, `jira`, `generate_image`, `transcribe_audio` |
| **Messaging** | `send_message`, `send_notification` |
| **Orchestration** | `delegate_agent`, `orchestrate_agents`, `skill_router` |

Full list with names and parameters: [reference/feature-matrix.md](../reference/feature-matrix.md).

## Sandboxing

`shell_exec` and `code_interpreter` can be routed through Docker via `shell_exec_policies` in `cyntr.yaml`. Untrusted tenants get a `--network none`, read-only, resource-capped container per call. See [reference/config.md](../reference/config.md#shell_exec_policies).

## Related

- [How-to: Add a tool](../how-to/add-a-tool.md)
- [Concepts: Policy](policy.md) — every tool call passes through here.
- [Reference: Feature matrix](../reference/feature-matrix.md)
