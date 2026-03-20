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

## Why Cyntr

AI agents are powerful but dangerous in enterprise settings. [OpenClaw](https://openclaw.ai) has 250K+ GitHub stars but no security team — 1,200+ malicious skills found, API keys leaked across 135,000 exposed instances, and no multi-tenant isolation.

Cyntr is the enterprise security layer:

- **Deny-by-default policy engine** — every agent action is checked before execution
- **Tamper-evident audit logging** — SHA-256 hash chains with Ed25519 signing
- **Multi-tenant isolation** — namespace, process, or Docker container isolation per team
- **Workflow automation** — chain agent actions with conditions, retries, and webhooks
- **7 messaging channels** — Slack, Teams, WhatsApp, Telegram, Discord, Email, Webhook
- **5 LLM providers** — Claude, GPT, Gemini, Ollama, and any OpenAI-compatible API
- **OpenClaw compatible** — import SKILL.md files with automatic sandboxing

## Quick Start

```bash
# Clone and build
git clone https://github.com/surya-koritala/cyntr.git
cd cyntr
go build -o cyntr ./cmd/cyntr

# Interactive setup wizard
./cyntr init

# Start the server
ANTHROPIC_API_KEY=sk-ant-... ./cyntr start
```

Dashboard opens at **http://localhost:7700**

Or use the one-line installer:
```bash
curl -fsSL https://raw.githubusercontent.com/surya-koritala/cyntr/main/install.sh | sh
```

## What You Can Do

### Chat with AI agents via API
```bash
# Create an agent
curl -X POST http://localhost:7700/api/v1/tenants/my-org/agents \
  -H "Content-Type: application/json" \
  -d '{"name":"assistant","model":"claude","system_prompt":"You are helpful.","tools":["browse_web","file_read","github"]}'

# Chat
curl -X POST http://localhost:7700/api/v1/tenants/my-org/agents/assistant/chat \
  -H "Content-Type: application/json" \
  -d '{"message":"Check the weather in New York"}'
```

### Chat via Slack
Set `SLACK_BOT_TOKEN` and your Cyntr agent responds directly in Slack channels — with full tool access (web browsing, file ops, GitHub, Jira).

### Automate with workflows
```bash
# Register a workflow: fetch repo info -> write report
curl -X POST http://localhost:7700/api/v1/workflows \
  -d '{"name":"repo-report","tenant":"my-org","start_step":"fetch",
       "steps":[
         {"id":"fetch","type":"agent_chat","config":{"agent":"assistant","message":"Browse github.com/my-org/repo and summarize it"},"on_success":"write"},
         {"id":"write","type":"agent_chat","config":{"agent":"assistant","message":"Write this summary to /tmp/report.txt: {{fetch.output}}"}}
       ]}'

# Run it
curl -X POST http://localhost:7700/api/v1/workflows/wf_1/run
```

### Use the CLI
```bash
cyntr init                    # Setup wizard
cyntr start                   # Start server
cyntr doctor                  # Validate configuration
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
| Cross-channel identity | Same user recognized across all platforms |

### Security & Compliance
| Feature | Details |
|---------|---------|
| Policy engine | YAML rules — allow/deny/require_approval per tenant, agent, tool |
| RBAC | 4 built-in roles + custom roles with 11 permissions |
| Authentication | OIDC (Okta/Auth0/Azure AD), JWT sessions, API keys |
| API auth middleware | Bearer token validation on all endpoints |
| Audit logging | Tamper-evident SHA-256 hash chains, daily rotation, Ed25519 signing |
| Exec approvals | Human-in-the-loop queue with approve/deny API |
| Spending controls | Per-tenant and per-agent API cost budgets |
| Rate limiting | Per-tenant token bucket on the proxy gateway |
| Notifications | Slack webhook + log alerts for approvals, denials, errors |

### Enterprise
| Feature | Details |
|---------|---------|
| Multi-tenancy | Isolated agents, policies, audit trails, resource quotas |
| 3 isolation modes | Namespace (goroutines), Process (OS), Docker (containers) |
| Federation | Peer-to-peer multi-site with policy sync and federated audit queries |
| Data residency | Per-tenant node enforcement |
| Proxy gateway | Reverse proxy with Anthropic/OpenAI intent extraction |
| OpenClaw compat | Import SKILL.md with automatic restricted sandbox |
| Skill hot-reload | File watcher auto-detects changes |
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

Every component is a **kernel module** communicating via an in-process IPC bus with backpressure. Modules are booted in dependency order via topological sort.

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

  - name: require-approval-shell
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: require_approval
    priority: 20
```

### Environment Variables

**LLM Providers** (at least one required):
```bash
ANTHROPIC_API_KEY=sk-ant-...     # Claude
OPENAI_API_KEY=sk-...            # GPT
GEMINI_API_KEY=...               # Gemini
OLLAMA_URL=http://localhost:11434 # Local models
```

**Channels** (all optional):
```bash
SLACK_BOT_TOKEN=xoxb-...
TEAMS_APP_ID=...
TELEGRAM_BOT_TOKEN=...
DISCORD_BOT_TOKEN=...
WHATSAPP_ACCESS_TOKEN=...
EMAIL_SMTP_HOST=smtp.example.com
```

**Proxy** (for OpenClaw integration):
```bash
PROXY_UPSTREAM_URL=https://api.anthropic.com  # default
```

## API Reference

All endpoints return a consistent JSON envelope:
```json
{
  "data": { ... },
  "meta": { "request_id": "abc123", "timestamp": "2026-03-20T..." },
  "error": null
}
```

### System
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/system/health` | Module health status |
| GET | `/api/v1/system/version` | Server version |

### Agents
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/tenants` | List tenants |
| POST | `/api/v1/tenants/{tid}/agents` | Create agent |
| POST | `/api/v1/tenants/{tid}/agents/{name}/chat` | Chat with agent |
| GET | `/api/v1/tenants/{tid}/agents/{name}/stream` | SSE streaming chat |

### Security
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/policies/test` | Dry-run policy check |
| GET | `/api/v1/approvals` | List pending approvals |
| POST | `/api/v1/approvals/{id}/approve` | Approve action |
| POST | `/api/v1/approvals/{id}/deny` | Deny action |
| GET | `/api/v1/auth/oidc/login` | OIDC login |
| GET | `/api/v1/auth/oidc/callback` | OIDC callback |

### Skills
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/skills` | List installed skills |
| POST | `/api/v1/skills` | Install skill |
| POST | `/api/v1/skills/import/openclaw` | Import OpenClaw SKILL.md |

### Audit & Federation
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/audit` | Query audit logs |
| GET | `/api/v1/federation/peers` | List peers |
| POST | `/api/v1/federation/peers` | Join peer |

### Workflows
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/workflows` | Register workflow |
| GET | `/api/v1/workflows` | List workflows |
| POST | `/api/v1/workflows/{id}/run` | Run workflow |
| GET | `/api/v1/workflows/runs/{id}` | Get run status |

## Project Structure

```
cyntr/
├── cmd/cyntr/           # CLI (init, start, doctor, subcommands)
├── kernel/              # Microkernel (IPC bus, config, resources, logging)
├── modules/
│   ├── agent/           # AI runtime (providers, tools, sessions, memory)
│   │   ├── providers/   # Claude, GPT, Gemini, Ollama, Mock
│   │   └── tools/       # shell, http, file, browser, github, jira
│   ├── audit/           # Tamper-evident logging (hash chains, signing, rotation)
│   ├── channel/         # Messaging (Slack, Teams, WhatsApp, Telegram, Discord, Email)
│   ├── federation/      # Multi-site (peers, policy sync, federated queries)
│   ├── notify/          # Notification system (Slack webhook, log)
│   ├── policy/          # Security (rules, approvals, spending)
│   ├── proxy/           # Reverse proxy (intent extraction, rate limiting)
│   ├── scheduler/       # Cron jobs
│   ├── skill/           # Skill management (loader, registry, OpenClaw compat)
│   └── workflow/        # Workflow engine (steps, conditions, webhooks)
├── auth/                # RBAC, OIDC, JWT, API keys, identity binding
├── tenant/              # Multi-tenancy (process supervisor, Docker sandbox)
├── web/                 # REST API + embedded dashboard
│   └── api/             # 22 endpoints with auth middleware
├── tests/integration/   # End-to-end tests
├── LICENSE              # Apache 2.0
├── CONTRIBUTING.md      # Development guidelines
└── install.sh           # One-line installer
```

## Development

```bash
# Run all tests (505 tests across 32 packages)
go test ./... -race

# Build
go build -o cyntr ./cmd/cyntr

# Lint
go vet ./...

# Verify setup
./cyntr doctor
```

### Testing Philosophy
- **No mocks** — all tests use real SQLite, real IPC, real HTTP (httptest)
- **TDD** — tests written before implementation
- **Live integration tests** — real Claude API, real GitHub API, real Slack
- **Race detection** — all tests pass with `-race`

## OpenClaw Integration

Cyntr works as a security proxy in front of OpenClaw. No changes to OpenClaw needed — just redirect its API calls:

```bash
# In your OpenClaw config, set:
ANTHROPIC_BASE_URL=http://cyntr-host:9080

# Cyntr will:
# 1. Extract intent from each API call (model, tools)
# 2. Check policy (allow/deny/require_approval)
# 3. Log to audit trail
# 4. Forward allowed requests to the real API
# 5. Rate limit per tenant
```

Import OpenClaw skills with automatic sandboxing:
```bash
cyntr skill import-openclaw ./path/to/SKILL.md
# Skills get:
# - "openclaw-" name prefix (no collisions)
# - No shell access (regardless of what skill declares)
# - Filesystem restricted to /tmp
# - No network access
# - Marked as unsigned/untrusted
```

## Comparison with OpenClaw

| Feature | OpenClaw | Cyntr |
|---------|----------|-------|
| **Model** | Personal assistant | Enterprise platform |
| **Security** | Optional exec-approvals | Deny-by-default policy engine |
| **Audit** | None | Tamper-evident hash chains |
| **Multi-tenant** | Single operator | Full tenant isolation |
| **Authentication** | Password-based | OIDC/SAML + RBAC |
| **Federation** | None | Multi-site with policy sync |
| **Compliance** | None | SOC 2, GDPR, HIPAA ready |
| **Skill security** | 20% malicious on ClawHub | Capability-declared + sandboxed |
| **Deployment** | Node.js process | Single Go binary |
| **Channels** | 20+ | 7 + extensible |
| **Workflow** | None | Multi-step with conditions |

## Roadmap

- [ ] Production WASM sandbox (wazero) for skill execution
- [ ] Full Playwright browser automation
- [ ] Skill marketplace / remote registry
- [ ] Voice message transcription
- [ ] Mobile companion app
- [ ] Google Chat adapter
- [ ] Additional channel adapters (Signal, IRC, Matrix)

## License

[Apache License 2.0](LICENSE)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

---

<p align="center">
  Built with security-first principles for enterprise AI.
</p>
