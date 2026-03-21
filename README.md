<p align="center">
  <h1 align="center">Cyntr</h1>
  <p align="center"><strong>Open-Source AI Agent Platform for Enterprise</strong></p>
  <p align="center">Deploy AI agents that run shell commands, browse the web, query databases, manage cloud infrastructure, and automate workflows — all behind policy-enforced security with full audit trails.</p>
  <p align="center">
    <a href="https://github.com/surya-koritala/cyntr/releases"><img src="https://img.shields.io/github/v/release/surya-koritala/cyntr" alt="Release"></a>
    <a href="https://github.com/surya-koritala/cyntr/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License"></a>
    <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go" alt="Go">
    <img src="https://img.shields.io/badge/tests-passing-brightgreen" alt="Tests">
    <img src="https://img.shields.io/badge/tools-20-orange" alt="Tools">
    <img src="https://img.shields.io/badge/providers-8-purple" alt="Providers">
    <img src="https://img.shields.io/badge/channels-9-blue" alt="Channels">
  </p>
</p>

---

## Why Cyntr?

Most AI agent frameworks are libraries — you build around them. Cyntr is a **platform** — you deploy it and it runs your agents. Single Go binary. Zero external dependencies. Self-hosted.

- **One agent, all tools** — use `["*"]` to give an agent access to every tool. New tools automatically available.
- **Slack-native** — agents respond in Slack with typing indicators, progress messages, and auto-chunked responses.
- **Cloud infrastructure ops** — agents run AWS/Azure/GCP CLI commands with read-only policy enforcement.
- **Enterprise security** — every tool call checked against policy rules. Deny by default. Full audit trail.
- **No vendor lock-in** — 8 LLM providers. Switch models without changing agent code.

---

## Quick Start

```bash
# Clone and build
git clone https://github.com/surya-koritala/cyntr.git
cd cyntr && go build -o cyntr ./cmd/cyntr

# Interactive 5-step setup wizard
./cyntr init

# Start the server
set -a && source .env && set +a
./cyntr start
```

Dashboard opens at **http://localhost:7700**

The setup wizard configures your AI provider, messaging channels, cloud CLI access, and security policy — all in one flow.

---

## Features

### 8 AI Model Providers

| Provider | Models | Auth |
|----------|--------|------|
| **Anthropic** | Claude 4, Sonnet, Haiku (streaming) | `ANTHROPIC_API_KEY` |
| **OpenAI** | GPT-4o, GPT-4, GPT-3.5 | `OPENAI_API_KEY` |
| **Azure OpenAI** | Azure AI Foundry deployments | `AZURE_OPENAI_API_KEY` + endpoint |
| **Google Gemini** | Gemini Pro, Flash | `GEMINI_API_KEY` |
| **OpenRouter** | 100+ models via single key | `OPENROUTER_API_KEY` |
| **Ollama** | Llama, Mistral, CodeLlama (local) | `OLLAMA_URL` |
| **Mock** | Testing and development | Always available |

### 20 Agent Tools

| Category | Tools |
|----------|-------|
| **System** | `shell_exec` (bash, 120s timeout), `code_interpreter` (Python/JS) |
| **Files** | `file_read`, `file_write`, `file_search` |
| **Web** | `browse_web`, `advanced_browse`, `chromium_browser` (headless Chrome), `web_search` (Google), `http_request` |
| **Data** | `database_query` (SQLite + PostgreSQL, read-only), `pdf_reader`, `knowledge_search` (RAG with FTS5) |
| **Integrations** | `github` (PRs/issues), `jira` (tickets), `generate_image` (DALL-E), `transcribe_audio` (Whisper) |
| **Orchestration** | `delegate_agent`, `orchestrate_agents` (parallel multi-agent) |
| **Custom** | Define tools in `tools/*.yaml` — no Go code required |

### 9 Messaging Channels

| Channel | Integration |
|---------|------------|
| **Slack** | Events API + typing indicator + progress messages + response chunking |
| **Microsoft Teams** | Bot Framework + Adaptive Cards |
| **Discord** | Bot API |
| **Telegram** | Bot API webhook |
| **WhatsApp** | Business Cloud API |
| **Email** | SMTP outbound + webhook inbound |
| **Google Chat** | Webhook adapter |
| **Webhook** | Generic HTTP POST (any platform) |

### Slack-Native Experience

- **Typing indicator** — hourglass emoji while agent works
- **Progress messages** — "Running `shell_exec`..." sent during tool execution
- **Response chunking** — auto-splits messages over 4000 chars with `[1/N]` indicators
- **Session management** — type `clear` or `reset` to start a fresh conversation
- **Multi-channel routing** — different Slack channels route to different agents
- **Approval notifications** — `require_approval` policy sends to designated channel
- **Scheduled reports** — cron job results delivered to Slack

### Cloud Infrastructure Operations

Agents run AWS, Azure, and GCP CLI commands directly — configured during onboarding:

```
You: List all ECS clusters running in us-east-1
Cyntr: Running `aws ecs list-clusters --region us-east-1`...

Found 1 cluster:
- arn:aws:ecs:us-east-1:712416033:cluster/production-api

Want me to list the services and tasks inside this cluster?
```

- **Read-only by default** — system prompt + policy rules prevent modifications
- **CLI auth check** — `cyntr doctor` verifies AWS/Azure/GCP CLIs are installed and authenticated
- **Configurable security** — deny all / require approval / cloud-ops only / allow all

### Security & Policy Engine

```yaml
# policy.yaml
rules:
  - name: deny-shell-global
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 20

  - name: allow-shell-cloudops
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "cloud-ops"
    decision: allow
    priority: 30
```

| Feature | Description |
|---------|-------------|
| **Policy engine** | YAML rules — allow / deny / require_approval per tenant, agent, tool |
| **Secret masking** | AWS keys, Slack tokens, GitHub tokens, JWTs, passwords auto-redacted |
| **API authentication** | API key generated during init, Bearer token auth |
| **RBAC** | 4 built-in roles (admin, team_lead, user, auditor) with 11 permissions |
| **Audit logging** | SHA-256 hash chains for tamper-evident logs |
| **Approval queue** | Human-in-the-loop with approve/deny via dashboard or Slack |
| **Rate limiting** | Per-tenant token bucket on proxy gateway |

### 15-Page Dashboard

| Page | What it does |
|------|-------------|
| **Dashboard** | Health cards, module status, recent audit, job/skill/agent counts |
| **Agents** | Create, edit (model/prompt/tools), delete, chat interface |
| **Sessions** | Browse conversation history per agent, view message details |
| **Memories** | View/delete agent long-term memories |
| **Knowledge** | Upload documents for RAG search, manage knowledge base |
| **Skills** | Install, uninstall, OpenClaw import, marketplace search |
| **Workflows** | Register multi-step workflows, run, view step-by-step progress |
| **Scheduler** | Create cron jobs with channel delivery, view LastRun/NextRun |
| **Audit** | Filter by tenant/user/action/agent/date range, CSV export |
| **Policies** | View loaded rules, test policy decisions |
| **Approvals** | Review pending approvals, approve/deny with context |
| **Channels** | Active adapter status, configuration guide |
| **Tenants** | Create, configure, delete tenants |
| **Federation** | Add/remove peer nodes, sync status |
| **Logs** | Real-time SSE activity stream (tool executions, errors) |

### Knowledge Base (RAG)

Ingest internal documents and let agents search them — powered by SQLite FTS5, no external vector database needed:

```bash
# Upload a document
curl -X POST localhost:7700/api/v1/knowledge \
  -d '{"title":"Deploy Guide","content":"How to deploy to production...","tags":"devops"}'

# Agent searches automatically via knowledge_search tool
You: How do we deploy to production?
Cyntr: According to the Deploy Guide: ...
```

### Custom YAML Tools

Define tools without writing Go code:

```yaml
# tools/check_disk.yaml
name: check_disk
description: Check disk usage on the system
parameters:
  path:
    type: string
    description: Filesystem path to check
    required: false
command: "df -h {{.path}}"
timeout: 10s
```

Tools in `tools/` are loaded automatically at startup.

### Multi-Agent Orchestration

Dispatch tasks to multiple agents in parallel:

```bash
You: Compare our AWS costs across all three environments

# Agent uses orchestrate_agents tool internally:
# [{"tenant":"ops","agent":"aws-dev","message":"Get cost summary"},
#  {"tenant":"ops","agent":"aws-staging","message":"Get cost summary"},
#  {"tenant":"ops","agent":"aws-prod","message":"Get cost summary"}]
```

### Workflow Engine

Chain agent actions with conditions, retries, webhooks, and delays:

```json
{
  "name": "incident-response",
  "steps": [
    {"id": "detect", "type": "agent_chat", "config": {"agent": "monitor", "message": "Check error rates"}},
    {"id": "diagnose", "type": "agent_chat", "config": {"agent": "cloud-ops", "message": "Investigate: {{detect.output}}"}},
    {"id": "notify", "type": "webhook", "config": {"url": "https://hooks.slack.com/...", "method": "POST"}}
  ]
}
```

Step types: `agent_chat`, `tool_call`, `condition`, `webhook`, `delay`, `approval`

### Federation

Connect multiple Cyntr instances for cross-site agent communication:

- **Policy sync** — rules propagated across peers
- **Federated audit** — query logs across all connected nodes
- **Agent delegation** — agents on one node can delegate to agents on another

---

## Architecture

```
                         ┌──────────────────────────────────┐
                         │   CLI · Dashboard · REST API · SDK │
                         └──────────────┬───────────────────┘
                                        │
                         ┌──────────────▼───────────────────┐
                         │            KERNEL                  │
                         │   IPC Bus · Config · Resources     │
                         └──────────────┬───────────────────┘
                                        │
    ┌────────┬────────┬────────┬────────┼────────┬────────┬────────┬────────┐
    │        │        │        │        │        │        │        │        │
 Policy   Audit   Agent    Channel  Proxy    Skill    Fed.    Sched.  Workflow
 Engine   Logger  Runtime  Manager  Gateway  Runtime  Module  Module  Engine
```

Every component is a **kernel module** communicating via an in-process IPC bus. Modules boot in dependency order and shut down in reverse.

- **No external databases** — SQLite for sessions, memory, audit, knowledge
- **No message queues** — IPC bus with backpressure
- **No container runtime** — single binary deployment
- **No configuration service** — YAML files + environment variables

---

## SDKs

### Python

```python
from cyntr import CyntrClient

client = CyntrClient("http://localhost:7700", api_key="cyntr_...")

# Chat with an agent
response = client.chat("my-org", "assistant", "What's running in us-east-1?")
print(response["content"])

# Manage knowledge base
client.ingest_knowledge("Runbook", "Steps to restart the service...", "ops")
```

Install: `pip install ./sdk/python`

### JavaScript

```javascript
const { CyntrClient } = require('@cyntr/sdk');

const client = new CyntrClient('http://localhost:7700', 'cyntr_...');

const response = await client.chat('my-org', 'assistant', 'List S3 buckets');
console.log(response.content);

// Stream responses
const stream = client.chatStream('my-org', 'assistant', 'Analyze logs');
stream.addEventListener('message', (e) => console.log(JSON.parse(e.data)));
```

Install: `npm install ./sdk/js`

---

## API Reference

All endpoints return: `{"data": ..., "meta": {"request_id", "timestamp"}, "error": null}`

### System
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/system/health` | Module health status |
| GET | `/api/v1/system/version` | Version info |

### Tenants
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/tenants` | List all tenants |
| GET | `/api/v1/tenants/{tid}` | Get tenant details |
| POST | `/api/v1/tenants` | Create tenant |
| DELETE | `/api/v1/tenants/{tid}` | Delete tenant |

### Agents
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/tenants/{tid}/agents` | List agents |
| POST | `/api/v1/tenants/{tid}/agents` | Create agent |
| GET | `/api/v1/tenants/{tid}/agents/{name}` | Get agent config |
| PUT | `/api/v1/tenants/{tid}/agents/{name}` | Update agent |
| DELETE | `/api/v1/tenants/{tid}/agents/{name}` | Delete agent |
| POST | `/api/v1/tenants/{tid}/agents/{name}/chat` | Chat with agent |
| GET | `/api/v1/tenants/{tid}/agents/{name}/stream` | SSE streaming chat |
| GET | `/api/v1/tenants/{tid}/agents/{name}/sessions` | List sessions |
| GET | `/api/v1/tenants/{tid}/agents/{name}/sessions/{sid}/messages` | Get messages |
| GET | `/api/v1/tenants/{tid}/agents/{name}/memories` | List memories |
| DELETE | `/api/v1/tenants/{tid}/agents/{name}/memories/{mid}` | Delete memory |

### Policies & Approvals
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/policies/rules` | List loaded rules |
| POST | `/api/v1/policies/test` | Test policy decision |
| GET | `/api/v1/approvals` | List pending approvals |
| POST | `/api/v1/approvals/{id}/approve` | Approve action |
| POST | `/api/v1/approvals/{id}/deny` | Deny action |

### Skills
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/skills` | List installed skills |
| POST | `/api/v1/skills` | Install skill from path |
| DELETE | `/api/v1/skills/{name}` | Uninstall skill |
| POST | `/api/v1/skills/import/openclaw` | Import OpenClaw skill |

### Knowledge Base
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/knowledge` | List documents |
| POST | `/api/v1/knowledge` | Ingest document |
| DELETE | `/api/v1/knowledge/{id}` | Delete document |

### Workflows
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/workflows` | List workflows |
| POST | `/api/v1/workflows` | Register workflow |
| GET | `/api/v1/workflows/{id}` | Get workflow definition |
| POST | `/api/v1/workflows/{id}/run` | Execute workflow |
| GET | `/api/v1/workflows/runs` | List all runs |
| GET | `/api/v1/workflows/runs/{id}` | Get run status |

### Schedules
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/schedules` | List jobs |
| POST | `/api/v1/schedules` | Create job |
| POST | `/api/v1/schedules/{id}/remove` | Remove job |

### Audit & Federation
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/audit` | Query audit log (filter: tenant, user, action, agent, since, until, limit) |
| GET | `/api/v1/channels` | List active channel adapters |
| GET | `/api/v1/federation/peers` | List federation peers |
| POST | `/api/v1/federation/peers` | Join federation |
| DELETE | `/api/v1/federation/peers/{name}` | Remove peer |

---

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

### Environment Variables

**LLM Providers** (at least one):
```bash
ANTHROPIC_API_KEY=sk-ant-...          # Claude
OPENAI_API_KEY=sk-...                 # GPT
AZURE_OPENAI_API_KEY=...              # Azure AI Foundry
AZURE_OPENAI_ENDPOINT=https://...     # Azure endpoint
AZURE_OPENAI_DEPLOYMENT=gpt-4o       # Azure deployment
GEMINI_API_KEY=...                    # Gemini
OPENROUTER_API_KEY=...                # OpenRouter (100+ models)
OLLAMA_URL=http://localhost:11434     # Local models
```

**Channels** (optional):
```bash
SLACK_BOT_TOKEN=xoxb-...             # Slack
SLACK_ROUTES=C123=cloud-ops,C456=bot # Per-channel agent routing
SLACK_APPROVAL_CHANNEL=C789          # Approval notifications
TEAMS_APP_ID=...                     # Microsoft Teams
TELEGRAM_BOT_TOKEN=...               # Telegram
DISCORD_BOT_TOKEN=...                # Discord
WHATSAPP_ACCESS_TOKEN=...            # WhatsApp
EMAIL_SMTP_HOST=smtp.example.com     # Email
GOOGLE_CHAT_WEBHOOK_URL=...          # Google Chat
```

**Security**:
```bash
CYNTR_API_KEY=cyntr_...              # API authentication (auto-generated by init)
```

---

## CLI Reference

```bash
cyntr init              # 5-step interactive setup wizard
cyntr start [config]    # Start server (auto-registers cloud-ops agent)
cyntr doctor            # Validate config, check cloud CLI auth
cyntr version           # Show version
cyntr status            # Server health check

cyntr agent create <tenant> <name> --model <provider>
cyntr agent chat <tenant> <name> "message"

cyntr tenant list
cyntr audit query --tenant finance
cyntr policy test --tenant demo --action tool_call --tool shell_exec
cyntr skill list
cyntr skill import-openclaw ./path/to/skill
cyntr federation peers
```

---

## Development

```bash
# Build
go build -o cyntr ./cmd/cyntr

# Test (33 packages)
go test ./... -count=1 -race

# Validate setup
./cyntr doctor
```

---

## Comparison

| Feature | Cyntr | LangChain | CrewAI | AutoGen |
|---------|-------|-----------|--------|---------|
| Self-hosted platform | Yes | Library | Library | Library |
| Single binary | Yes | No | No | No |
| Policy engine | Yes | No | No | No |
| Audit logging | Yes | No | No | No |
| Multi-tenant | Yes | No | No | No |
| Slack/Teams/Discord | Built-in | Plugin | No | No |
| Dashboard | Built-in | No | No | No |
| Cloud ops (AWS/Azure/GCP) | Built-in | Plugin | No | No |
| Federation | Yes | No | No | No |
| Zero dependencies | Yes | Many | Many | Many |

---

## License

[Apache License 2.0](LICENSE)

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md)

---

<p align="center">
  <a href="https://github.com/surya-koritala/cyntr">GitHub</a> ·
  <a href="https://github.com/surya-koritala/cyntr/releases">Releases</a> ·
  <a href="https://github.com/surya-koritala/cyntr/issues">Issues</a>
</p>

<p align="center">
  <sub>Built with Go. No frameworks. No dependencies. Just code.</sub>
</p>
