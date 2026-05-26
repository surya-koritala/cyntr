[Cyntr Docs](../README.md) > How-to > Add a tool

# Add a tool

Two paths: YAML for shell-wrapping tools (no Go required), Go for tools that need real logic.

## Path 1 — YAML tool

```yaml
# tools/check_disk.yaml
name: check_disk
description: Check disk usage on the system.
parameters:
  path:
    type: string
    description: Filesystem path to check.
    required: false
command: "df -h {{.path}}"
timeout: 10s
```

Drop the file in `tools/`, restart cyntr (or hot-reload via `POST /api/v1/tools/reload`). The tool is now selectable in any agent's tool grant list. Parameters are validated and templated into the command; `timeout` is a hard cap.

YAML tools run under the same `shell_exec_policies` sandbox as `shell_exec` — see [reference/config.md](../reference/config.md#shell_exec_policies).

## Path 2 — Go tool

Implement `agent.Tool`:

```go
type MyTool struct{}

func (t *MyTool) Name() string { return "my_tool" }
func (t *MyTool) Description() string { return "..." }
func (t *MyTool) Schema() agent.ToolSchema { return agent.ToolSchema{...} }
func (t *MyTool) Execute(ctx context.Context, args map[string]any) (agent.ToolResult, error) {
    // ...
}
```

Register in `cmd/cyntr/main.go`:

```go
toolReg.Register(&MyTool{})
```

Rebuild. The tool is now available.

## Related

- [Concepts: Tools](../concepts/tools.md)
- [Concepts: Policy](../concepts/policy.md) — every tool call passes through here.
- [How-to: Write a policy](write-a-policy.md)
