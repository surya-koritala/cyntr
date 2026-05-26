[Cyntr Docs](../README.md) > How-to > Configure LLM providers

# Configure LLM providers

Cyntr supports 8 providers. Set the env var for the one you want to use; restart cyntr. Multiple providers can be configured simultaneously — agents pick by `model` name.

## Provider env vars

```bash
ANTHROPIC_API_KEY=sk-ant-...          # Claude (claude-sonnet-4, claude-haiku, etc.)
OPENAI_API_KEY=sk-...                 # GPT-4o, GPT-4, GPT-3.5
AZURE_OPENAI_API_KEY=...              # Azure AI Foundry deployments
AZURE_OPENAI_ENDPOINT=https://...
AZURE_OPENAI_DEPLOYMENT=gpt-4o
GEMINI_API_KEY=...                    # Gemini Pro, Flash
OPENROUTER_API_KEY=...                # 100+ models via one key
OLLAMA_URL=http://localhost:11434     # Local models
```

The `mock` provider is always available — useful for tests and local dev without spending tokens.

## Multiple Anthropic models

```bash
ANTHROPIC_MODELS=claude-sonnet-4,claude-haiku-4,claude-opus-4
```

Each model registers as a separate provider entry. Agents reference them by exact model string.

## Switching an agent's model

```bash
curl -X PUT localhost:7700/api/v1/tenants/demo/agents/assistant \
  -d '{"model": "claude-haiku-4"}'
```

This creates a new agent version. Roll back with `POST .../rollback/v1` if needed.

## Cost tracking

Every chat turn writes a `usage` record: provider, model, input tokens, output tokens, agent, tenant. Query via `GET /api/v1/usage` or aggregate via `GET /api/v1/usage/summary`. See [reference/api.md](../reference/api.md#usage--metrics).

## Related

- [Reference: Env vars](../reference/env-vars.md#llm-providers)
- [Reference: Feature matrix — providers](../reference/feature-matrix.md#providers)
