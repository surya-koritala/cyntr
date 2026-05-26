[Cyntr Docs](../README.md) > Getting Started > First Agent

# Your first agent

At the end of this page you'll have an agent named `assistant`, talking to it from three places: the `cyntr chat` CLI, the REST API, and a Slack channel. Total time: about 5 minutes.

Prerequisite: Cyntr running locally. See [install](install.md) if it isn't.

## 1. Create a tenant

A tenant is the policy and isolation boundary that everything else hangs off. We'll create one called `demo`.

```bash
curl -X POST localhost:7700/api/v1/tenants \
  -H "Content-Type: application/json" \
  -d '{"name": "demo", "isolation": "namespace"}'
```

Expected response:

```json
{"data": {"name": "demo", "isolation": "namespace"}, "error": null}
```

## 2. Create the agent

```bash
cyntr agent create demo assistant --model claude-sonnet-4
```

This registers an agent with the default system prompt, the default tool grant, and a 10-turn cap. The agent is immediately reachable on the API and any configured channels.

Inspect it:

```bash
curl localhost:7700/api/v1/tenants/demo/agents/assistant
```

You'll see model, prompt, tools, skills, and version (`v1`). Every edit creates a new version you can roll back to — see [agents concept](../concepts/agents.md).

## 3. Chat via CLI

```bash
cyntr agent chat demo assistant "What is 2+2?"
```

Expected output:

```json
{
  "data": {"content": "4", "tokens": {"input": 12, "output": 1}},
  "error": null
}
```

## 4. Chat via REST API

```bash
curl -X POST localhost:7700/api/v1/tenants/demo/agents/assistant/chat \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $CYNTR_API_KEY" \
  -d '{"message": "Summarize the docs/ directory"}'
```

For streaming responses (Server-Sent Events):

```bash
curl -N "localhost:7700/api/v1/tenants/demo/agents/assistant/stream?message=hello" \
  -H "X-API-Key: $CYNTR_API_KEY"
```

See [reference/api.md](../reference/api.md) for envelope shape and every endpoint.

## 5. Chat via Slack

If you ran `cyntr init` with Slack credentials, your agent is already addressable in Slack. Otherwise:

```bash
export SLACK_BOT_TOKEN=xoxb-...
export SLACK_SIGNING_SECRET=...
export SLACK_ROUTES="C0123456=assistant"     # this channel → this agent
./cyntr start
```

Now in Slack channel `#C0123456`:

```
you   > @cyntr what's running in us-east-1?
cyntr > Running `aws ecs list-clusters`...
        Found 1 cluster: production-api
```

Slack-specific behaviors (threads, slash commands, reactions for approvals) are covered in [how-to/add-a-channel.md](../how-to/add-a-channel.md).

## What just happened

```
you  -> CLI/API/Slack  -> kernel.IPC -> agent.runtime
                                          |
                                          v
                                  policy check (per turn, per tool)
                                          |
                                          v
                                  LLM provider + tool calls
                                          |
                                          v
                                       audit log (hash-chained)
```

Each call passed through the policy engine, the agent runtime issued tool calls, and every step was written to a tamper-evident audit log. See [concepts/architecture.md](../concepts/architecture.md) for the full path.

## Next steps

- **Try a vertical demo:** [`demos/cloud-ops/`](../../demos/cloud-ops/) ships a pre-configured agent that runs read-only AWS investigations end-to-end.
- **Lock it down:** [Write a policy](../how-to/write-a-policy.md) so this agent can't shell out in prod.
- **Add a tool:** [Add a custom YAML tool](../how-to/add-a-tool.md) — no Go required.
- **Test before deploying:** [Run evals](../how-to/run-evals.md) in CI.
