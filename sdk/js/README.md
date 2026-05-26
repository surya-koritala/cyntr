# @cyntr/sdk

JavaScript / TypeScript client for the [Cyntr](https://github.com/surya-koritala/cyntr) AI Agent Platform. Node 18+; zero dependencies (uses built-in `fetch` and `ReadableStream`).

## Install

```bash
npm install @cyntr/sdk
```

Or copy `src/` into your project.

## Quickstart

```js
const { CyntrClient, CyntrError } = require('@cyntr/sdk');

const client = new CyntrClient('http://localhost:7700', 'cyntr_...', {
  timeout: 60_000,
});

(async () => {
  // 1. List + create an agent
  await client.listAgents('acme');
  await client.createAgent('acme', 'researcher', 'claude-opus-4.7',
    'You are a careful researcher.',
    { tools: ['web_search', 'knowledge'], skills: ['citations'] });

  // 2. Chat (non-streaming)
  const reply = await client.chat('acme', 'researcher',
    "What's new in vector DBs?", { user: 'alice' });
  console.log(reply.content);

  // 3. Chat (streaming) — async iterator over SSE events
  for await (const event of client.chatStream('acme', 'researcher', 'Explain GraphRAG')) {
    if (event.type === 'token') process.stdout.write(event.content);
  }

  // 4. Ingest + search knowledge
  await client.ingestKnowledge('pgvector vs Qdrant', '...', ['vector', 'rag']);
  const hits = await client.searchKnowledge('vector index', 5);

  // 5. Run an eval
  const runId = await client.runEval('researcher', 'acme', [
    { name: 'basic', prompt: '2+2',          expected: '4'    },
    { name: 'cite',  prompt: 'Cite RFC 2119', expected: 'MUST' },
  ]);
  const status = await client.getEvalRun(runId);
  console.log(`${status.passed} / ${status.total}`);

  // 6. Run a workflow
  await client.runWorkflow('acme', 'nightly_summary', { date: '2026-05-25' });
  await client.getWorkflowRun('wfr_abc');

  // 7. Audit trail
  await client.queryAudit({ tenant: 'acme', action: 'chat', limit: 50 });
})();
```

## TypeScript

Types ship in `src/index.d.ts`. Every method, options object, and model is typed — including `chatStream` as `AsyncGenerator<StreamEvent>`.

## Error handling

All non-2xx responses throw a `CyntrError` carrying `status`, `code`, `requestId`, and `message`:

```js
try {
  await client.getAgent('acme', 'missing');
} catch (e) {
  if (e instanceof CyntrError) {
    console.error(e.status, e.code, e.requestId, e.message);
  }
}
```

5xx and connection errors retry with exponential backoff (configurable via `maxRetries` and `retryBackoff`).

## Testing

```bash
node --test sdk/js/test/*.test.js
```

Tests use Node's built-in `node:test` runner; `globalThis.fetch` is monkey-patched to a mock — no live server required, no Jest, no Mocha.
