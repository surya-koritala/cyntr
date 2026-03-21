/**
 * Cyntr JavaScript SDK — client for the Cyntr AI Agent Platform API.
 *
 * Usage:
 *   const { CyntrClient } = require('@cyntr/sdk');
 *   const client = new CyntrClient('http://localhost:7700', 'cyntr_...');
 *   const tenants = await client.listTenants();
 *   const response = await client.chat('demo', 'bot', 'Hello!');
 */

class CyntrClient {
  constructor(baseUrl = 'http://localhost:7700', apiKey = null) {
    this.baseUrl = baseUrl.replace(/\/+$/, '');
    this.apiKey = apiKey;
  }

  async _request(method, path, body = null) {
    const headers = { 'Content-Type': 'application/json' };
    if (this.apiKey) headers['Authorization'] = `Bearer ${this.apiKey}`;

    const opts = { method, headers };
    if (body) opts.body = JSON.stringify(body);

    const resp = await fetch(`${this.baseUrl}${path}`, opts);
    const json = await resp.json().catch(() => ({}));

    if (!resp.ok) {
      const msg = json?.error?.message || json?.message || `HTTP ${resp.status}`;
      throw new Error(`Cyntr API error: ${msg}`);
    }
    return json.data ?? json;
  }

  // System
  health() { return this._request('GET', '/api/v1/system/health'); }
  version() { return this._request('GET', '/api/v1/system/version'); }

  // Tenants
  listTenants() { return this._request('GET', '/api/v1/tenants'); }
  getTenant(tenant) { return this._request('GET', `/api/v1/tenants/${tenant}`); }
  createTenant(name, isolation = 'namespace', policy = 'default') {
    return this._request('POST', '/api/v1/tenants', { name, isolation, policy });
  }
  deleteTenant(tenant) { return this._request('DELETE', `/api/v1/tenants/${tenant}`); }

  // Agents
  listAgents(tenant) { return this._request('GET', `/api/v1/tenants/${tenant}/agents`); }
  createAgent(tenant, config) { return this._request('POST', `/api/v1/tenants/${tenant}/agents`, config); }
  getAgent(tenant, name) { return this._request('GET', `/api/v1/tenants/${tenant}/agents/${name}`); }
  updateAgent(tenant, name, config) { return this._request('PUT', `/api/v1/tenants/${tenant}/agents/${name}`, config); }
  deleteAgent(tenant, name) { return this._request('DELETE', `/api/v1/tenants/${tenant}/agents/${name}`); }
  chat(tenant, agent, message) {
    return this._request('POST', `/api/v1/tenants/${tenant}/agents/${agent}/chat`, { message });
  }

  // Sessions & Memories
  listSessions(tenant, agent) { return this._request('GET', `/api/v1/tenants/${tenant}/agents/${agent}/sessions`); }
  getSessionMessages(tenant, agent, sid) {
    return this._request('GET', `/api/v1/tenants/${tenant}/agents/${agent}/sessions/${sid}/messages`);
  }
  listMemories(tenant, agent) { return this._request('GET', `/api/v1/tenants/${tenant}/agents/${agent}/memories`); }
  deleteMemory(tenant, agent, mid) {
    return this._request('DELETE', `/api/v1/tenants/${tenant}/agents/${agent}/memories/${mid}`);
  }

  // Skills
  listSkills() { return this._request('GET', '/api/v1/skills'); }
  installSkill(path) { return this._request('POST', '/api/v1/skills', { path }); }
  uninstallSkill(name) { return this._request('DELETE', `/api/v1/skills/${name}`); }

  // Policies
  listPolicyRules() { return this._request('GET', '/api/v1/policies/rules'); }
  testPolicy(body) { return this._request('POST', '/api/v1/policies/test', body); }

  // Workflows
  listWorkflows() { return this._request('GET', '/api/v1/workflows'); }
  getWorkflow(id) { return this._request('GET', `/api/v1/workflows/${id}`); }
  registerWorkflow(def) { return this._request('POST', '/api/v1/workflows', def); }
  runWorkflow(id) { return this._request('POST', `/api/v1/workflows/${id}/run`, {}); }
  listWorkflowRuns() { return this._request('GET', '/api/v1/workflows/runs'); }
  getWorkflowRun(id) { return this._request('GET', `/api/v1/workflows/runs/${id}`); }

  // Schedules
  listSchedules() { return this._request('GET', '/api/v1/schedules'); }
  addSchedule(body) { return this._request('POST', '/api/v1/schedules', body); }
  removeSchedule(id) { return this._request('POST', `/api/v1/schedules/${id}/remove`); }

  // Audit
  queryAudit(params = {}) {
    const qs = Object.entries(params).filter(([,v]) => v).map(([k,v]) => `${k}=${v}`).join('&');
    return this._request('GET', `/api/v1/audit${qs ? '?' + qs : ''}`);
  }

  // Federation
  listPeers() { return this._request('GET', '/api/v1/federation/peers'); }
  joinPeer(name, endpoint, secret = '') {
    return this._request('POST', '/api/v1/federation/peers', { name, endpoint, secret });
  }
  removePeer(name) { return this._request('DELETE', `/api/v1/federation/peers/${name}`); }

  // Approvals
  listApprovals() { return this._request('GET', '/api/v1/approvals'); }
  approve(id, decidedBy = '') { return this._request('POST', `/api/v1/approvals/${id}/approve`, { decided_by: decidedBy }); }
  deny(id, decidedBy = '') { return this._request('POST', `/api/v1/approvals/${id}/deny`, { decided_by: decidedBy }); }

  // Channels
  listChannels() { return this._request('GET', '/api/v1/channels'); }

  // Knowledge
  listKnowledge() { return this._request('GET', '/api/v1/knowledge'); }
  ingestKnowledge(title, content, tags = '') {
    return this._request('POST', '/api/v1/knowledge', { title, content, tags });
  }
  deleteKnowledge(id) { return this._request('DELETE', `/api/v1/knowledge/${id}`); }

  // SSE streaming
  chatStream(tenant, agent, message) {
    const url = `${this.baseUrl}/api/v1/tenants/${tenant}/agents/${agent}/stream?message=${encodeURIComponent(message)}`;
    return new EventSource(url);
  }
}

module.exports = { CyntrClient };
