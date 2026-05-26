[Cyntr Docs](../README.md) > Concepts > Agents

# Agents

An agent in Cyntr is a versioned record that bundles: a model, a system prompt, a set of allowed tools, a set of attached skills, and runtime knobs (max turns, rate limits, channel routing).

## The four parts

```yaml
agent:
  model: "claude-sonnet-4"           # which LLM
  prompt: "You are a careful..."     # system prompt; supports {{user}}, {{date}}, {{tenant}}, {{agent}} templates
  tools: [shell_exec, web_search]    # explicit grant; agents can only call tools in this list
  skills: [incident-response]        # bundles that add prompt + tools on demand
  max_turns: 10                      # runaway-loop guard
  rate_limit:
    requests_per_min: 30
```

Stored under `/api/v1/tenants/<t>/agents/<name>`. Every edit creates a new version (`v1`, `v2`, ...); rollback is one API call. The dashboard's Versions page lists the history.

## Lifecycle of a chat turn

1. Channel adapter or API receives the message.
2. Runtime loads the agent record + session history (sliding window, summarized if needed).
3. Policy check on the chat itself.
4. LLM call with the system prompt, history, and tool schemas.
5. If the LLM returns tool calls: each one is policy-checked, executed, results appended.
6. Loop until the LLM returns a final message, `max_turns` is hit, or a tool denial chain terminates.
7. Response sent back. Session, usage, and audit log updated atomically.

## Sessions and memory

- **Sessions** are per-(tenant, agent, user) conversation buffers. SQLite-backed, summarized automatically as they grow.
- **Memory** is opt-in long-term storage the agent reads and writes via the `user_model_read` / `user_model_write` tools. Separate TTL.
- **Usage** is per-turn token accounting per provider, per agent. Drives quotas.

Default TTLs: sessions 90d, memory 180d, usage 365d. Override via env vars; cleanup runs every 24h.

## Related

- [How-to: Add a tool](../how-to/add-a-tool.md)
- [How-to: Add a skill](../how-to/add-a-skill.md)
- [Concepts: Tools](tools.md)
- [Concepts: Skills](skills.md)
- [Reference: API — agents](../reference/api.md#agents)
