[Cyntr Docs](../README.md) > Concepts > Multi-tenant

# Multi-tenant

Tenants are the top-level isolation boundary in Cyntr. Every agent, session, policy decision, audit entry, and quota is scoped to a tenant. You can run a single tenant and ignore the rest of this page, but if you're running agents on behalf of multiple teams or customers, this is where the model pays off.

## Isolation modes

| Mode | Boundary | Cost |
|------|----------|------|
| `namespace` | Logical — agents share the host process, but session/audit/memory queries are tenant-scoped. | Free. |
| `process` | Each tenant gets its own child process supervised by the kernel. | ~30MB RAM per tenant + IPC overhead. |

Choose `process` when you need OS-level isolation between tenants (e.g. a misbehaving agent in tenant A must not be able to read tenant B's data even through a memory bug). For most deployments `namespace` is correct.

## RBAC

Four built-in roles, 11 permissions:

| Role | Can |
|------|-----|
| `admin` | Everything in their tenant: manage agents, policies, users, channels. |
| `team_lead` | Create/edit agents, approve actions, view audit. No user management. |
| `user` | Chat with agents, view their own sessions. |
| `auditor` | Read-only access to audit log, policy decisions, usage. |

Permissions are enforced per HTTP method. The dashboard hides what your role can't reach.

## OIDC / SSO

Set `CYNTR_OIDC_ISSUER` and `CYNTR_OIDC_CLIENT_ID`. The dashboard redirects to your IdP; users land back with a session cookie. API access still uses API keys (issue them scoped — `read`, `agent`, `admin`).

PKCE is required. Tested against Auth0, Okta, Azure AD, Keycloak, Google Workspace.

## Quotas

Set on the tenant in `cyntr.yaml`:

```yaml
tenants:
  customer-acme:
    quota:
      tokens_per_day: 1000000
      requests_per_min: 60
```

When the budget is exhausted, the proxy returns 429. The agent receives a structured error and can choose to wait or surface it to the user. Rolling 24h window; reset is per-tenant local midnight (UTC by default).

## Per-tenant policy

Each tenant can point at its own policy file:

```yaml
tenants:
  prod-finance:
    policy: "policies/finance.yaml"
  prod-marketing:
    policy: "policies/marketing.yaml"
```

The global `policy.yaml` is the fallback. Tenant-specific Rego is also supported via per-tenant `policy.rego` paths.

## What's tenant-scoped, what isn't

| Scoped | Not scoped |
|--------|------------|
| Agents | LLM provider API keys |
| Sessions | Tool registry |
| Memory | Channel adapter credentials |
| Audit log | Federation peers |
| Usage records | Server config |
| Policy decisions | OIDC config |

Channel adapters are global, but channel-to-tenant routing happens per message — see [how-to/add-a-channel.md](../how-to/add-a-channel.md) for `SLACK_ROUTES`-style maps.

## Related

- [Getting started: Deploy](../getting-started/deploy.md) — production checklist.
- [Concepts: Policy](policy.md) — per-tenant policy.
- [Reference: Config](../reference/config.md#tenants) — full schema.
