# Cyntr

The enterprise AI agent platform. Open source.

Cyntr is a self-hosted platform for running, securing, and managing AI agents across your organization. Built in Go as a single binary with zero external dependencies.

## Why Cyntr

AI agents are powerful but dangerous in enterprise settings. OpenClaw has 250K+ GitHub stars but no security team — 1,200 malicious skills, API key leaks, and exposed instances across 82 countries.

Cyntr solves this with:
- **Deny-by-default policy engine** — every action is checked before execution
- **Tamper-evident audit logging** — cryptographic hash chains for compliance (SOC 2, GDPR, HIPAA)
- **Multi-tenant isolation** — namespace, process, or Docker container isolation per team
- **Federation** — multi-site deployment with policy sync and cross-site audit queries

## Quick Start

```bash
# Build from source
git clone https://github.com/surya-koritala/cyntr.git
cd cyntr
go build -o cyntr ./cmd/cyntr

# Interactive setup
./cyntr init

# Start
./cyntr start
```

Dashboard opens at **http://localhost:7700**

## Features

### AI Runtime
- **5 model providers** — Claude, GPT, Gemini, Ollama, and any OpenAI-compatible API
- **Agentic tool loop** — model calls tools, tools return results, repeat until done
- **6 built-in tools** — shell, HTTP, file read/write/search, web browser
- **Long-term memory** — agents remember across sessions via SQLite
- **Streaming** — Server-Sent Events for real-time token delivery
- **Scheduled jobs** — cron-style recurring agent tasks

### Messaging Channels
- **7 adapters** — Slack, Microsoft Teams, WhatsApp, Telegram, Discord, Email, Webhook
- **Cross-channel identity** — same user recognized across all platforms
- **OpenClaw SKILL.md compatibility** — import existing skills with sandboxed execution

### Security & Compliance
- **Policy engine** — YAML rules with allow/deny/require_approval per tenant, agent, tool
- **RBAC** — 4 built-in roles (admin, team_lead, user, auditor) + custom roles
- **OIDC authentication** — enterprise SSO via Okta, Auth0, Azure AD, Google Workspace
- **JWT sessions + API keys** — for web UI and automation
- **Spending controls** — per-tenant and per-agent API cost budgets
- **Exec approval queue** — human-in-the-loop for sensitive actions
- **Rate limiting** — per-tenant token bucket on the proxy gateway

### Enterprise
- **Multi-tenant** — each team gets isolated agents, policies, audit trails, and resource quotas
- **3 isolation modes** — namespace (goroutines), process (OS processes), Docker containers
- **Federation** — peer-to-peer multi-site with policy sync and federated audit queries
- **Data residency** — enforce which node stores which tenant's data
- **Proxy gateway** — reverse proxy with intent extraction (Anthropic, OpenAI protocol parsers)
- **Config migration** — versioned schema upgrades built into the binary

### Interface
- **REST API** — 20+ endpoints with consistent JSON envelope responses
- **Web dashboard** — agent chat, audit log viewer, policy tester, federation management
- **CLI** — `cyntr init`, `cyntr start`, `cyntr doctor`, `cyntr agent chat`, `cyntr audit query`, and more
- **SSE events** — real-time dashboard updates

## Architecture

```
                     ┌─────────────────────────────┐
                     │   CLI / Dashboard / API      │
                     └──────────────┬──────────────┘
                                    │
                     ┌──────────────▼──────────────┐
                     │          KERNEL              │
                     │   IPC Bus · Config · Resources│
                     └──────────────┬──────────────┘
                                    │
     ┌──────────┬──────────┬────────┼────────┬──────────┬──────────┐
     │          │          │        │        │          │          │
  Policy    Audit     Agent     Channel   Proxy     Skill    Federation
  Engine    Logger    Runtime   Manager   Gateway   Runtime   Module
```

Every component is a **kernel module** communicating via an in-process IPC bus. Modules are booted in dependency order via topological sort and shut down in reverse order.

## Configuration

### cyntr.yaml
```yaml
version: "1"
listen:
  address: "127.0.0.1:8080"
  webui: ":7700"
tenants:
  engineering:
    isolation: namespace
    policy: default
  finance:
    isolation: process
    policy: strict
```

### policy.yaml
```yaml
rules:
  - name: allow-model-calls
    tenant: "*"
    action: model_call
    tool: "*"
    agent: "*"
    decision: allow
    priority: 10

  - name: deny-shell-finance
    tenant: finance
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 30
```

### Environment Variables
```bash
# LLM Providers (at least one required)
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
GEMINI_API_KEY=...
OLLAMA_URL=http://localhost:11434

# Channels (all optional)
SLACK_BOT_TOKEN=xoxb-...
TEAMS_APP_ID=...
TELEGRAM_BOT_TOKEN=...
DISCORD_BOT_TOKEN=...
WHATSAPP_ACCESS_TOKEN=...
EMAIL_SMTP_HOST=smtp.example.com
```

## CLI Reference

```bash
cyntr init              # Interactive setup wizard
cyntr start [config]    # Start the server
cyntr doctor            # Check configuration
cyntr status            # Show server health
cyntr version           # Show version

cyntr tenant list
cyntr agent create <tenant> <name> --model <provider>
cyntr agent chat <tenant> <agent> <message>
cyntr audit query --tenant <name>
cyntr policy test --tenant <t> --action <a> --tool <tool>
cyntr skill list
cyntr skill import-openclaw <path>
cyntr federation peers
```

## API

All endpoints return a consistent JSON envelope:
```json
{
  "data": { ... },
  "meta": { "request_id": "...", "timestamp": "..." },
  "error": null
}
```

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/system/health` | GET | Module health status |
| `/api/v1/system/version` | GET | Server version |
| `/api/v1/tenants` | GET | List tenants |
| `/api/v1/tenants/{tid}/agents` | POST | Create agent |
| `/api/v1/tenants/{tid}/agents/{name}/chat` | POST | Chat with agent |
| `/api/v1/tenants/{tid}/agents/{name}/stream` | GET | SSE streaming chat |
| `/api/v1/policies/test` | POST | Dry-run policy check |
| `/api/v1/skills` | GET | List installed skills |
| `/api/v1/skills/import/openclaw` | POST | Import OpenClaw skill |
| `/api/v1/audit` | GET | Query audit logs |
| `/api/v1/federation/peers` | GET/POST | Manage federation peers |
| `/api/v1/approvals` | GET | List pending approvals |
| `/api/v1/approvals/{id}/approve` | POST | Approve action |
| `/api/v1/approvals/{id}/deny` | POST | Deny action |
| `/api/v1/auth/oidc/login` | GET | OIDC login |
| `/api/v1/auth/oidc/callback` | GET | OIDC callback |

## Development

```bash
# Run tests (460 tests across 29 packages)
go test ./... -race

# Build
go build -o cyntr ./cmd/cyntr

# Verify
go vet ./...
```

## Project Structure

```
cyntr/
├── cmd/cyntr/          # CLI entrypoint
├── kernel/             # Microkernel core (IPC, config, resources)
├── modules/
│   ├── agent/          # AI runtime (providers, tools, sessions, memory)
│   ├── audit/          # Tamper-evident logging
│   ├── channel/        # Messaging adapters (Slack, Teams, etc.)
│   ├── federation/     # Multi-site distribution
│   ├── policy/         # Security policy engine
│   ├── proxy/          # HTTP reverse proxy
│   ├── scheduler/      # Cron job scheduler
│   └── skill/          # Skill management + OpenClaw compat
├── auth/               # RBAC, OIDC, JWT, API keys
├── tenant/             # Multi-tenancy, process isolation, Docker
├── web/                # REST API + dashboard
└── tests/integration/  # End-to-end tests
```

## License

Apache License 2.0 — see [LICENSE](LICENSE)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md)
