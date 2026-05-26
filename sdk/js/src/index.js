/**
 * Cyntr JavaScript SDK — client for the Cyntr AI Agent Platform API.
 *
 * Requires Node 18+ (built-in ``fetch`` + ``ReadableStream``). No
 * third-party dependencies.
 *
 * Usage:
 *   const { CyntrClient } = require('@cyntr/sdk');
 *   const client = new CyntrClient('http://localhost:7700', 'cyntr_...');
 *   const tenants = await client.listTenants();
 *   const reply = await client.chat('demo', 'bot', 'Hello!');
 */

'use strict';

class CyntrError extends Error {
  constructor(status, code, message, requestId = '') {
    super(`Cyntr API error ${status} (${code}): ${message}`);
    this.name = 'CyntrError';
    this.status = status;
    this.code = code;
    this.requestId = requestId;
  }
}

class CyntrClient {
  /**
   * @param {string} baseUrl
   * @param {string|null} apiKey
   * @param {{ timeout?: number, maxRetries?: number, retryBackoff?: number }} [opts]
   */
  constructor(baseUrl = 'http://localhost:7700', apiKey = null, opts = {}) {
    this.baseUrl = baseUrl.replace(/\/+$/, '');
    this.apiKey = apiKey;
    this.timeout = opts.timeout ?? 60_000;
    this.maxRetries = opts.maxRetries ?? 3;
    this.retryBackoff = opts.retryBackoff ?? 1000;
  }

  _headers(extra = {}) {
    const h = { 'Content-Type': 'application/json', Accept: 'application/json', ...extra };
    if (this.apiKey) h['Authorization'] = `Bearer ${this.apiKey}`;
    return h;
  }

  async _request(method, path, body = null) {
    const url = `${this.baseUrl}${path}`;
    const init = { method, headers: this._headers() };
    if (body !== null && body !== undefined) init.body = JSON.stringify(body);

    let lastErr;
    for (let attempt = 0; attempt <= this.maxRetries; attempt++) {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), this.timeout);
      try {
        const resp = await fetch(url, { ...init, signal: controller.signal });
        clearTimeout(timer);
        let payload;
        try { payload = await resp.json(); } catch { payload = {}; }
        if (!resp.ok) {
          if (resp.status >= 500 && attempt < this.maxRetries) {
            await new Promise(r => setTimeout(r, this.retryBackoff * (2 ** attempt)));
            lastErr = new CyntrError(resp.status, '', `HTTP ${resp.status}`);
            continue;
          }
          const err = payload?.error || {};
          const reqId = payload?.meta?.request_id || '';
          throw new CyntrError(resp.status, err.code || '', err.message || `HTTP ${resp.status}`, reqId);
        }
        return payload?.data ?? payload;
      } catch (e) {
        clearTimeout(timer);
        if (e instanceof CyntrError) throw e;
        if (attempt < this.maxRetries) {
          await new Promise(r => setTimeout(r, this.retryBackoff * (2 ** attempt)));
          lastErr = e;
          continue;
        }
        throw new CyntrError(0, 'CONNECTION_ERROR', e.message || String(e));
      }
    }
    throw new CyntrError(0, 'MAX_RETRIES', `max retries exceeded: ${lastErr}`);
  }

  _qs(params) {
    const entries = Object.entries(params).filter(([, v]) => v !== undefined && v !== null && v !== '' && v !== 0);
    if (!entries.length) return '';
    return '?' + new URLSearchParams(entries).toString();
  }

  // System
  health() { return this._request('GET', '/api/v1/system/health'); }
  version() { return this._request('GET', '/api/v1/system/version'); }

  // Tenants
  listTenants() { return this._request('GET', '/api/v1/tenants'); }
  getTenant(tid) { return this._request('GET', `/api/v1/tenants/${tid}`); }
  createTenant(name, isolation = 'namespace', policy = 'default') {
    return this._request('POST', '/api/v1/tenants', { name, isolation, policy });
  }
  deleteTenant(tid) { return this._request('DELETE', `/api/v1/tenants/${tid}`); }

  // Agents
  listAgents(tenant) { return this._request('GET', `/api/v1/tenants/${tenant}/agents`); }
  createAgent(tenant, nameOrConfig, model, prompt, opts = {}) {
    // Two forms supported:
    //   createAgent(tenant, { name, model, system_prompt, tools, skills })
    //   createAgent(tenant, name, model, prompt, { tools, skills })
    let body;
    if (typeof nameOrConfig === 'object' && nameOrConfig !== null) {
      body = nameOrConfig;
    } else {
      body = { name: nameOrConfig };
      if (model !== undefined) body.model = model;
      if (prompt !== undefined) body.system_prompt = prompt;
      if (opts.tools !== undefined) body.tools = opts.tools;
      if (opts.skills !== undefined) body.skills = opts.skills;
    }
    return this._request('POST', `/api/v1/tenants/${tenant}/agents`, body);
  }
  getAgent(tenant, name) { return this._request('GET', `/api/v1/tenants/${tenant}/agents/${name}`); }
  updateAgent(tenant, name, fields) { return this._request('PUT', `/api/v1/tenants/${tenant}/agents/${name}`, fields); }
  deleteAgent(tenant, name) { return this._request('DELETE', `/api/v1/tenants/${tenant}/agents/${name}`); }

  chat(tenant, agent, message, { user, channel } = {}) {
    const body = { message };
    if (user !== undefined) body.user = user;
    if (channel !== undefined) body.channel = channel;
    return this._request('POST', `/api/v1/tenants/${tenant}/agents/${agent}/chat`, body);
  }

  /**
   * Stream chat tokens via Server-Sent Events.
   * Yields the parsed JSON of each ``data:`` line as an async iterator.
   */
  async *chatStream(tenant, agent, message) {
    const url = `${this.baseUrl}/api/v1/tenants/${tenant}/agents/${agent}/stream?message=${encodeURIComponent(message)}`;
    const resp = await fetch(url, { headers: this._headers({ Accept: 'text/event-stream' }) });
    if (!resp.ok) {
      throw new CyntrError(resp.status, 'STREAM_FAILED', `stream HTTP ${resp.status}`);
    }
    if (!resp.body) {
      throw new CyntrError(0, 'NO_BODY', 'streaming response has no body');
    }
    const reader = resp.body.getReader();
    const decoder = new TextDecoder('utf-8');
    let buf = '';
    try {
      for (;;) {
        const { value, done } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        let idx;
        while ((idx = buf.indexOf('\n\n')) !== -1) {
          const raw = buf.slice(0, idx);
          buf = buf.slice(idx + 2);
          for (const line of raw.split('\n')) {
            if (!line.startsWith('data:')) continue;
            const data = line.slice(5).trim();
            if (!data) continue;
            try { yield JSON.parse(data); }
            catch { yield { raw: data }; }
          }
        }
      }
    } finally {
      try { reader.releaseLock(); } catch { /* noop */ }
    }
  }

  // Sessions
  listSessions(tenant, agent) {
    return this._request('GET', `/api/v1/tenants/${tenant}/agents/${agent}/sessions`);
  }
  getSession(tenant, agent, sid) {
    return this._request('GET', `/api/v1/tenants/${tenant}/agents/${agent}/sessions/${sid}/messages`);
  }
  // Legacy alias
  getSessionMessages(tenant, agent, sid) { return this.getSession(tenant, agent, sid); }
  clearSession(tenant, agent, sid = 'current') {
    return this._request('DELETE', `/api/v1/tenants/${tenant}/agents/${agent}/sessions/${sid}`);
  }

  // Memories
  listMemories(tenant, agent) {
    return this._request('GET', `/api/v1/tenants/${tenant}/agents/${agent}/memories`);
  }
  deleteMemory(tenant, agent, mid) {
    return this._request('DELETE', `/api/v1/tenants/${tenant}/agents/${agent}/memories/${mid}`);
  }

  // Skills
  listSkills() { return this._request('GET', '/api/v1/skills'); }
  installSkill(name) { return this._request('POST', '/api/v1/skills', { path: name }); }
  uninstallSkill(name) { return this._request('DELETE', `/api/v1/skills/${name}`); }
  searchSkills(query) {
    return this._request('GET', `/api/v1/skills/marketplace/search?q=${encodeURIComponent(query)}`);
  }

  // Workflows
  listWorkflows(_tenant) { return this._request('GET', '/api/v1/workflows'); }
  getWorkflow(id) { return this._request('GET', `/api/v1/workflows/${id}`); }
  registerWorkflow(def) { return this._request('POST', '/api/v1/workflows', def); }
  runWorkflow(tenant, name, inputs = {}) {
    void tenant; // accepted for parity
    return this._request('POST', `/api/v1/workflows/${name}/run`, inputs);
  }
  listWorkflowRuns() { return this._request('GET', '/api/v1/workflows/runs'); }
  getWorkflowRun(id) { return this._request('GET', `/api/v1/workflows/runs/${id}`); }

  // Knowledge
  listKnowledge() { return this._request('GET', '/api/v1/knowledge'); }
  ingestKnowledge(title, content, tags = null) {
    const tagStr = Array.isArray(tags) ? tags.join(',') : (tags || '');
    return this._request('POST', '/api/v1/knowledge', { title, content, tags: tagStr });
  }
  searchKnowledge(query, limit = 10) {
    return this._request('GET', `/api/v1/knowledge/search?q=${encodeURIComponent(query)}&limit=${limit}`);
  }
  deleteKnowledge(id) { return this._request('DELETE', `/api/v1/knowledge/${id}`); }

  // Audit
  queryAudit({ tenant, user, action, since, limit = 100 } = {}) {
    const qs = this._qs({ tenant, user, action, since, limit });
    return this._request('GET', `/api/v1/audit${qs}`);
  }

  // Eval
  async runEval(agent, tenant, cases) {
    const resp = await this._request('POST', '/api/v1/eval/run', { agent, tenant, cases });
    if (resp && typeof resp === 'object') return resp.run_id || resp.id || '';
    return '';
  }
  getEvalRun(id) { return this._request('GET', `/api/v1/eval/runs/${id}`); }
  listEvalRuns() { return this._request('GET', '/api/v1/eval/runs'); }

  // Policies
  listPolicyRules() { return this._request('GET', '/api/v1/policies/rules'); }
  testPolicy(body) { return this._request('POST', '/api/v1/policies/test', body); }

  // Schedules / Federation / Approvals / Channels
  listSchedules() { return this._request('GET', '/api/v1/schedules'); }
  addSchedule(body) { return this._request('POST', '/api/v1/schedules', body); }
  removeSchedule(id) { return this._request('POST', `/api/v1/schedules/${id}/remove`); }

  listPeers() { return this._request('GET', '/api/v1/federation/peers'); }
  joinPeer(name, endpoint, secret = '') {
    return this._request('POST', '/api/v1/federation/peers', { name, endpoint, secret });
  }
  removePeer(name) { return this._request('DELETE', `/api/v1/federation/peers/${name}`); }

  listApprovals() { return this._request('GET', '/api/v1/approvals'); }
  approve(id, decidedBy = '') { return this._request('POST', `/api/v1/approvals/${id}/approve`, { decided_by: decidedBy }); }
  deny(id, decidedBy = '') { return this._request('POST', `/api/v1/approvals/${id}/deny`, { decided_by: decidedBy }); }
  listChannels() { return this._request('GET', '/api/v1/channels'); }
}

module.exports = { CyntrClient, CyntrError };
