'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const { CyntrClient, CyntrError } = require('../src/index.js');

// Build an envelope-shaped JSON Response.
function envelope(data, { status = 200, error = null } = {}) {
  const body = JSON.stringify({
    data: error ? null : data,
    meta: { request_id: 'req_test', timestamp: '2026-01-01T00:00:00Z' },
    error,
  });
  return new Response(body, {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// Build a mock fetch that captures every call and returns a canned response.
function mockFetch(responder) {
  const calls = [];
  const fn = async (url, init) => {
    calls.push({ url, init });
    return responder(url, init);
  };
  fn.calls = calls;
  return fn;
}

test('listAgents returns parsed data and sends auth header', async () => {
  const fetchFn = mockFetch(() => envelope(['a', 'b']));
  globalThis.fetch = fetchFn;
  const c = new CyntrClient('http://localhost:7700', 'cyntr_test');
  const agents = await c.listAgents('acme');
  assert.deepEqual(agents, ['a', 'b']);
  assert.equal(fetchFn.calls.length, 1);
  const call = fetchFn.calls[0];
  assert.equal(call.init.method, 'GET');
  assert.match(String(call.url), /\/api\/v1\/tenants\/acme\/agents$/);
  assert.equal(call.init.headers['Authorization'], 'Bearer cyntr_test');
});

test('createAgent (kwarg form) builds correct body', async () => {
  const fetchFn = mockFetch(() => envelope({ status: 'created', agent: 'bot' }));
  globalThis.fetch = fetchFn;
  const c = new CyntrClient('http://localhost:7700', 'k');
  const out = await c.createAgent('acme', 'bot', 'claude', 'be terse', {
    tools: ['*'],
    skills: ['search'],
  });
  assert.equal(out.status, 'created');
  const sent = JSON.parse(fetchFn.calls[0].init.body);
  assert.deepEqual(sent, {
    name: 'bot',
    model: 'claude',
    system_prompt: 'be terse',
    tools: ['*'],
    skills: ['search'],
  });
});

test('chat sends user + channel and parses ChatResponse', async () => {
  const fetchFn = mockFetch(() => envelope({
    agent: 'bot', content: 'hi', tools_used: null,
  }));
  globalThis.fetch = fetchFn;
  const c = new CyntrClient('http://localhost:7700', 'k');
  const resp = await c.chat('acme', 'bot', 'hello', { user: 'u1', channel: 'slack' });
  assert.equal(resp.content, 'hi');
  const body = JSON.parse(fetchFn.calls[0].init.body);
  assert.deepEqual(body, { message: 'hello', user: 'u1', channel: 'slack' });
});

test('runWorkflow posts inputs and returns run_id', async () => {
  const fetchFn = mockFetch(() => envelope({ run_id: 'wfr_1' }));
  globalThis.fetch = fetchFn;
  const c = new CyntrClient();
  const out = await c.runWorkflow('acme', 'nightly', { x: 1 });
  assert.equal(out.run_id, 'wfr_1');
  assert.match(String(fetchFn.calls[0].url), /\/workflows\/nightly\/run$/);
  assert.deepEqual(JSON.parse(fetchFn.calls[0].init.body), { x: 1 });
});

test('runEval submits cases and returns run id string', async () => {
  const fetchFn = mockFetch(() => envelope({ run_id: 'ev_42' }));
  globalThis.fetch = fetchFn;
  const c = new CyntrClient();
  const rid = await c.runEval('bot', 'acme', [
    { name: 'c1', prompt: '2+2?', expected: '4' },
  ]);
  assert.equal(rid, 'ev_42');
  const body = JSON.parse(fetchFn.calls[0].init.body);
  assert.equal(body.agent, 'bot');
  assert.equal(body.tenant, 'acme');
  assert.equal(body.cases.length, 1);
  assert.equal(body.cases[0].name, 'c1');
});

test('error response raises CyntrError with code + requestId', async () => {
  globalThis.fetch = mockFetch(() => new Response(
    JSON.stringify({
      data: null,
      meta: { request_id: 'req_err' },
      error: { code: 'NOT_FOUND', message: 'agent missing' },
    }),
    { status: 404, headers: { 'Content-Type': 'application/json' } },
  ));
  const c = new CyntrClient('http://localhost:7700', 'k', { maxRetries: 0 });
  await assert.rejects(
    () => c.getAgent('acme', 'missing'),
    (err) => {
      assert.ok(err instanceof CyntrError);
      assert.equal(err.status, 404);
      assert.equal(err.code, 'NOT_FOUND');
      assert.equal(err.requestId, 'req_err');
      return true;
    },
  );
});

test('searchKnowledge encodes query and limit', async () => {
  const fetchFn = mockFetch(() => envelope([{ id: 'kb1' }]));
  globalThis.fetch = fetchFn;
  const c = new CyntrClient();
  await c.searchKnowledge('hello world', 5);
  const url = String(fetchFn.calls[0].url);
  assert.match(url, /q=hello%20world/);
  assert.match(url, /limit=5/);
});

test('queryAudit filters drop empty values', async () => {
  const fetchFn = mockFetch(() => envelope([]));
  globalThis.fetch = fetchFn;
  const c = new CyntrClient();
  await c.queryAudit({ tenant: 'acme', action: 'chat', limit: 50 });
  const url = String(fetchFn.calls[0].url);
  assert.match(url, /tenant=acme/);
  assert.match(url, /action=chat/);
  assert.match(url, /limit=50/);
  assert.doesNotMatch(url, /user=/);
});

test('chatStream parses SSE data events', async () => {
  // Build a ReadableStream that yields two SSE events then closes.
  const enc = new TextEncoder();
  const sse = enc.encode(
    'event: message\ndata: {"type":"thinking","content":""}\n\n' +
    'event: message\ndata: {"type":"token","content":"hi"}\n\n',
  );
  const stream = new ReadableStream({
    start(controller) {
      controller.enqueue(sse);
      controller.close();
    },
  });
  globalThis.fetch = async () => new Response(stream, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  });
  const c = new CyntrClient();
  const events = [];
  for await (const ev of c.chatStream('acme', 'bot', 'hi')) {
    events.push(ev);
  }
  assert.equal(events.length, 2);
  assert.equal(events[0].type, 'thinking');
  assert.equal(events[1].content, 'hi');
});

test('no Authorization header when apiKey is null', async () => {
  const fetchFn = mockFetch(() => envelope({ ok: true }));
  globalThis.fetch = fetchFn;
  const c = new CyntrClient('http://localhost:7700');
  await c.health();
  assert.equal(fetchFn.calls[0].init.headers['Authorization'], undefined);
});
