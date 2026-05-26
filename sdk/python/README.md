# Cyntr Python SDK

Stdlib-only client for the [Cyntr](https://github.com/surya-koritala/cyntr) AI Agent Platform. No third-party dependencies — works on Python 3.10+.

## Install

```bash
pip install -e sdk/python  # editable install from the monorepo
```

Or copy the `cyntr/` package directly into your project.

## Quickstart

```python
from cyntr import CyntrClient, EvalCase

client = CyntrClient(
    base_url="http://localhost:7700",
    api_key="cyntr_...",
    timeout=60,
)

# 1. List + create an agent
client.list_agents("acme")
client.create_agent(
    "acme",
    name="researcher",
    model="claude-opus-4.7",
    prompt="You are a careful researcher.",
    tools=["web_search", "knowledge"],
    skills=["citations"],
)

# 2. Chat (non-streaming)
reply = client.chat("acme", "researcher", "What's new in vector DBs?", user="alice")
print(reply["content"])

# 3. Chat (streaming) — async generator over SSE events
for event in client.chat_stream("acme", "researcher", "Explain GraphRAG"):
    if event.get("type") == "token":
        print(event["content"], end="", flush=True)

# 4. Ingest + search knowledge
client.ingest_knowledge(
    title="pgvector vs Qdrant",
    content="...",
    tags=["vector", "rag"],
)
hits = client.search_knowledge("vector index", limit=5)

# 5. Run an eval
run_id = client.run_eval(
    agent="researcher",
    tenant="acme",
    cases=[
        EvalCase(name="basic", prompt="2+2", expected="4"),
        EvalCase(name="cite",  prompt="Cite RFC 2119", expected="MUST"),
    ],
)
status = client.get_eval_run(run_id)
print(status.passed, "/", status.total)

# 6. Run a workflow + check status
client.run_workflow("acme", "nightly_summary", inputs={"date": "2026-05-25"})
client.get_workflow_run("wfr_abc")

# 7. Audit trail
client.query_audit(tenant="acme", action="chat", limit=50)
```

## Error handling

All non-2xx responses raise `CyntrError` (subclass of `Exception`):

```python
from cyntr import CyntrError

try:
    client.get_agent("acme", "missing")
except CyntrError as e:
    print(e.status, e.code, e.message, e.request_id)
```

5xx responses and connection errors retry with exponential backoff (configurable via `max_retries` and `retry_backoff`).

## Typed models

`cyntr.models` ships stdlib dataclasses for `Agent`, `Session`, `WorkflowRun`, `EvalRun`, `EvalCase`. The default methods return raw dicts; typed helpers like `get_agent_typed`, `get_workflow_run_typed`, and `get_eval_run` return dataclasses.

## Testing

```bash
python3 -m unittest discover sdk/python/tests/
```

Tests mock `urllib.request.urlopen` — no live server required, no extra dependencies.
