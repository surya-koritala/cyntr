[Cyntr Docs](../README.md) > Reference > REST API

# REST API reference

80+ endpoints across 15 resource groups. All responses use the envelope:

```json
{
  "data": <resource>,
  "meta": {"request_id": "...", "timestamp": "..."},
  "error": null
}
```

On error: `data` is null and `error` is `{"code": "...", "message": "..."}`.

## Auth

Send `X-API-Key: cyntr_...` on every request. Keys are scoped — `read`, `agent`, `admin`. OIDC-authenticated browser sessions use a cookie set by the dashboard.

```bash
curl localhost:7700/api/v1/system/health -H "X-API-Key: $CYNTR_API_KEY"
```

## System

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/system/health` | Module health status |
| GET | `/api/v1/system/version` | Version info |
| GET | `/api/v1/metrics` | OpenMetrics text |

## Tenants

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/tenants` | List |
| GET | `/api/v1/tenants/{tid}` | Get |
| POST | `/api/v1/tenants` | Create |
| DELETE | `/api/v1/tenants/{tid}` | Delete |

## Agents

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/tenants/{tid}/agents` | List |
| POST | `/api/v1/tenants/{tid}/agents` | Create |
| GET | `/api/v1/tenants/{tid}/agents/{name}` | Get |
| PUT | `/api/v1/tenants/{tid}/agents/{name}` | Update (new version) |
| DELETE | `/api/v1/tenants/{tid}/agents/{name}` | Delete |
| POST | `/api/v1/tenants/{tid}/agents/{name}/chat` | Chat (sync) |
| GET | `/api/v1/tenants/{tid}/agents/{name}/stream` | Chat (SSE) |
| GET | `/api/v1/tenants/{tid}/agents/{name}/sessions` | List sessions |
| GET | `/api/v1/tenants/{tid}/agents/{name}/sessions/{sid}/messages` | Session messages |
| DELETE | `/api/v1/tenants/{tid}/agents/{name}/sessions/{sid}` | Clear session |
| GET | `/api/v1/tenants/{tid}/agents/{name}/memories` | List memories |
| DELETE | `/api/v1/tenants/{tid}/agents/{name}/memories/{mid}` | Delete memory |
| GET | `/api/v1/tenants/{tid}/agents/{name}/versions` | Version history |
| POST | `/api/v1/tenants/{tid}/agents/{name}/rollback/{v}` | Rollback |

## Policies & Approvals

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/policies/rules` | List loaded rules |
| POST | `/api/v1/policies/test` | Test a decision |
| GET | `/api/v1/approvals` | Pending approvals |
| POST | `/api/v1/approvals/{id}/approve` | Approve |
| POST | `/api/v1/approvals/{id}/deny` | Deny |

## Skills

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/skills` | List installed |
| GET | `/api/v1/skills/catalog` | Browse built-in catalog |
| POST | `/api/v1/skills` | Install |
| DELETE | `/api/v1/skills/{name}` | Uninstall |
| POST | `/api/v1/skills/import/openclaw` | Import OpenClaw |
| GET | `/api/v1/skills/search?q=X` | GitHub search |

## Knowledge

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/knowledge` | List |
| GET | `/api/v1/knowledge/search?q=X&mode=hybrid` | Search |
| POST | `/api/v1/knowledge` | Ingest |
| DELETE | `/api/v1/knowledge/{id}` | Delete |

## Workflows

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/workflows` | List |
| POST | `/api/v1/workflows` | Register |
| GET | `/api/v1/workflows/{id}` | Get |
| POST | `/api/v1/workflows/{id}/run` | Execute |
| GET | `/api/v1/workflows/runs` | List runs |
| GET | `/api/v1/workflows/runs/{id}` | Run status |

## Schedules

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/schedules` | List |
| POST | `/api/v1/schedules` | Create |
| POST | `/api/v1/schedules/{id}/remove` | Remove |

## Audit & Federation

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/audit` | Query (filter: tenant, user, action, agent, since, until, limit) |
| GET | `/api/v1/channels` | Active adapters |
| GET | `/api/v1/federation/peers` | List peers |
| POST | `/api/v1/federation/peers` | Join |
| DELETE | `/api/v1/federation/peers/{name}` | Remove |
| POST | `/api/v1/federation/delegate` | Outbound delegate |
| POST | `/api/v1/federation/inbound/delegate` | Peer-facing inbound |
| GET | `/api/v1/federation/health` | Liveness |

## MCP

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/mcp/servers` | List |
| POST | `/api/v1/mcp/servers` | Add |
| DELETE | `/api/v1/mcp/servers/{name}` | Remove |
| GET | `/api/v1/mcp/servers/{name}/tools` | List tools |
| GET | `/api/v1/mcp/marketplace` | Browse |
| POST | `/api/v1/mcp/marketplace/install` | Install |

## Crews

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/crews` | Create |
| GET | `/api/v1/crews` | List |
| POST | `/api/v1/crews/{id}/run` | Run |
| GET | `/api/v1/crews/runs/{id}` | Run status |
| GET | `/api/v1/crews/runs` | List runs |

## Eval

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/eval/run` | Run |
| GET | `/api/v1/eval/runs/{id}` | Results |
| GET | `/api/v1/eval/runs` | List runs |

## Usage & Metrics

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/usage` | Query records |
| GET | `/api/v1/usage/summary` | Aggregated |
| GET | `/api/v1/metrics` | Prometheus text |
| GET | `/api/v1/branding` | White-label config |

## Webhooks

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/webhooks/trigger/{workflow_id}` | Trigger workflow |
| POST | `/api/v1/webhooks/agent/{tenant}/{agent}` | Send message to agent |

## Related

- [Reference: CLI](cli.md) — the CLI is a thin client over this API.
- [Reference: Config](config.md)
