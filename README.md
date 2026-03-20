<p align="center">
  <h1 align="center">Cyntr</h1>
  <p align="center">The enterprise AI agent platform. Open source.</p>
  <p align="center">
    <a href="https://github.com/surya-koritala/cyntr/releases"><img src="https://img.shields.io/github/v/release/surya-koritala/cyntr" alt="Release"></a>
    <a href="https://github.com/surya-koritala/cyntr/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License"></a>
    <img src="https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go" alt="Go">
    <img src="https://img.shields.io/badge/tests-505%20passing-brightgreen" alt="Tests">
  </p>
</p>

---

Cyntr is a self-hosted platform for running, securing, and managing AI agents across your organization. Single Go binary. Zero external dependencies. 22,000 lines of code. 505 tests.

## Install

```bash
curl -fsSL https://cyntr.dev/install.sh | sh
```

Or build from source:
```bash
git clone https://github.com/surya-koritala/cyntr.git
cd cyntr && go build -o cyntr ./cmd/cyntr
```

## Quick Start

```bash
# Interactive setup wizard
cyntr init

# Start the server
ANTHROPIC_API_KEY=sk-ant-... cyntr start
```

Dashboard opens at **http://localhost:7700**

## What Cyntr Does

Cyntr lets you deploy AI agents that can talk on Slack, browse the web, manage GitHub issues, write files, and automate workflows — all behind a deny-by-default policy engine with full audit logging.

- **Run agents** across Claude, GPT, Gemini, Ollama, or any OpenAI-compatible API
- **Connect channels** — Slack, Teams, WhatsApp, Telegram, Discord, Email
- **Use tools** — shell, HTTP, file ops, web browser, GitHub, Jira
- **Automate workflows** — chain agent actions with conditions, retries, and webhooks
- **Enforce security** — policy engine checks every action before execution
- **Log everything** — tamper-evident audit trails with hash chains
- **Isolate tenants** — separate agents, policies, and data per team
- **Scale across sites** — federation with policy sync and cross-site audit queries

## Usage

### Chat with an agent via API
```bash
# Create an agent with tools
curl -X POST http://localhost:7700/api/v1/tenants/my-org/agents \
  -H "Content-Type: application/json" \
  -d '{"name":"assistant","model":"claude","system_prompt":"You are helpful.",
       "tools":["browse_web","file_read","github"]}'

# Chat
curl -X POST http://localhost:7700/api/v1/tenants/my-org/agents/assistant/chat \
  -H "Content-Type: application/json" \
  -d '{"message":"Check the weather in New York"}'
```

### Chat via Slack
Set `SLACK_BOT_TOKEN` and your agent responds directly in Slack channels with full tool access.

### Automate with workflows
```bash
curl -X POST http://localhost:7700/api/v1/workflows \
  -d '{"name":"daily-report","tenant":"my-org","start_step":"fetch",
       "steps":[
         {"id":"fetch","type":"agent_chat",
          "config":{"agent":"assistant","message":"Browse our status page and summarize"},"on_success":"write"},
         {"id":"write","type":"agent_chat",
          "config":{"agent":"assistant","message":"Write summary to /tmp/report.txt: {{fetch.output}}"}}
       ]}'
```

### Use the CLI
```bash
cyntr init                    # Setup wizard
cyntr start                   # Start server
cyntr doctor                  # Validate config
cyntr agent create my-org bot --model claude
cyntr agent chat my-org bot "What's on my calendar?"
cyntr tenant list
cyntr audit query --tenant finance
cyntr policy test --tenant demo --action tool_call --tool shell_exec
cyntr skill import-openclaw ./my-skill/SKILL.md
cyntr federation peers
```

## Features

### AI Runtime
| Feature | Details |
|---------|---------|
| Model providers | Claude (streaming), GPT, Gemini, Ollama, Mock |
| Agent tools | shell, HTTP, file read/write/search, web browser, GitHub, Jira |
| Long-term memory | SQLite-backed — agents remember across sessions |
| Session persistence | Conversations + agent configs survive restarts |
| Workflow engine | Multi-step automation with conditions, retries, webhooks |
| Cron scheduler | Recurring agent tasks on a schedule |
| Streaming | Server-Sent Events for real-time token delivery |

### Messaging Channels
| Channel | Integration |
|---------|------------|
| Slack | Events API + chat.postMessage |
| Microsoft Teams | Bot Framework activities |
| WhatsApp | Cloud API webhook |
| Telegram | Bot API webhook |
| Discord | Interactions API |
| Email | SMTP outbound + webhook inbound |
| Webhook | Generic HTTP POST |

### Security & Compliance
| Feature | Details |
|---------|---------|
| Policy engine | YAML rules — allow/deny/require_approval per tenant, agent, tool |
| RBAC | 4 built-in roles + custom roles with 11 permissions |
| Authentication | OIDC, JWT sessions, API keys |
| Audit logging | SHA-256 hash chains, Ed25519 signing, daily rotation |
| Exec approvals | Human-in-the-loop with approve/deny API |
| Spending controls | Per-tenant and per-agent budget limits |
| Rate limiting | Per-tenant token bucket |
| Notifications | Slack webhook + log alerts |

### Enterprise
| Feature | Details |
|---------|---------|
| Multi-tenancy | Isolated agents, policies, audit, resource quotas |
| 3 isolation modes | Namespace, Process, Docker container |
| Federation | Peer-to-peer with policy sync and federated audit queries |
| Data residency | Per-tenant node enforcement |
| Proxy gateway | Reverse proxy with protocol-aware intent extraction |
| Config migration | Versioned schema upgrades |
| Structured logging | JSON logs with levels and module context |

## Architecture

```
                         ┌──────────────────────────────┐
                         │   CLI · Dashboard · REST API  │
                         └──────────────┬───────────────┘
                                        │
                         ┌──────────────▼───────────────┐
                         │           KERNEL              │
                         │  IPC Bus · Config · Resources  │
                         └──────────────┬───────────────┘
                                        │
    ┌────────┬────────┬────────┬────────┼────────┬────────┬────────┬────────┐
    │        │        │        │        │        │        │        │        │
 Policy   Audit   Agent    Channel  Proxy    Skill    Fed.    Sched.  Workflow
 Engine   Logger  Runtime  Manager  Gateway  Runtime  Module  Module  Engine
```

Every component is a **kernel module** communicating via an in-process IPC bus with backpressure. Modules are booted in dependency order and shut down in reverse.

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
  finance:
    isolation: process
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

  - name: require-approval-shell
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: require_approval
    priority: 20
```

### Environment Variables

**LLM Providers** (at least one):
```bash
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
GEMINI_API_KEY=...
OLLAMA_URL=http://localhost:11434
```

**Channels** (optional):
```bash
SLACK_BOT_TOKEN=xoxb-...
TEAMS_APP_ID=...
TELEGRAM_BOT_TOKEN=...
DISCORD_BOT_TOKEN=...
WHATSAPP_ACCESS_TOKEN=...
EMAIL_SMTP_HOST=smtp.example.com
```

## API Reference

All endpoints return a consistent JSON envelope:
```json
{"data": {...}, "meta": {"request_id": "...", "timestamp": "..."}, "error": null}
```

### System
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/system/health` | Module health |
| GET | `/api/v1/system/version` | Version |

### Agents
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/tenants` | List tenants |
| POST | `/api/v1/tenants/{tid}/agents` | Create agent |
| POST | `/api/v1/tenants/{tid}/agents/{name}/chat` | Chat |
| GET | `/api/v1/tenants/{tid}/agents/{name}/stream` | SSE streaming |

### Security
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/policies/test` | Dry-run policy check |
| GET | `/api/v1/approvals` | Pending approvals |
| POST | `/api/v1/approvals/{id}/approve` | Approve |
| POST | `/api/v1/approvals/{id}/deny` | Deny |

### Skills & Audit
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/skills` | List skills |
| POST | `/api/v1/skills/import/openclaw` | Import skill |
| GET | `/api/v1/audit` | Query audit logs |

### Workflows & Federation
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/workflows` | Register workflow |
| POST | `/api/v1/workflows/{id}/run` | Run workflow |
| GET | `/api/v1/workflows/runs/{id}` | Run status |
| GET | `/api/v1/federation/peers` | List peers |
| POST | `/api/v1/federation/peers` | Join peer |

## Development

```bash
go test ./... -race    # 505 tests, 32 packages
go build -o cyntr ./cmd/cyntr
go vet ./...
./cyntr doctor
```

## License

[Apache License 2.0](LICENSE)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md)

---

<p align="center">
  <a href="https://cyntr.dev">cyntr.dev</a> · <a href="https://github.com/surya-koritala/cyntr/releases">Releases</a> · <a href="https://github.com/surya-koritala/cyntr/issues">Issues</a>
</p>
