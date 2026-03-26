<p align="center">
  <h1 align="center">Cyntr</h1>
  <p align="center"><strong>Open-Source AI Agent Platform for Enterprise</strong></p>
  <p align="center">Deploy AI agents that run shell commands, browse the web, query databases, manage Kubernetes clusters, analyze cloud costs, enforce security policies, and automate workflows — with 25 built-in enterprise skills, a skill marketplace, and full audit trails.</p>
  <p align="center">
    <a href="https://github.com/surya-koritala/cyntr/releases"><img src="https://img.shields.io/badge/release-v1.1.0-green" alt="Release"></a>
    <a href="https://github.com/surya-koritala/cyntr/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License"></a>
    <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go" alt="Go">
    <img src="https://img.shields.io/badge/tests-37%20packages%20passing-brightgreen" alt="Tests">
    <img src="https://img.shields.io/badge/tools-29-orange" alt="Tools">
    <img src="https://img.shields.io/badge/skills-31-red" alt="Skills">
    <img src="https://img.shields.io/badge/providers-7-purple" alt="Providers">
    <img src="https://img.shields.io/badge/channels-7-blue" alt="Channels">
    <img src="https://img.shields.io/badge/MCP-8%20servers-teal" alt="MCP">
    <img src="https://img.shields.io/badge/API-80%2B%20endpoints-yellow" alt="API">
  </p>
</p>

---

## Why Cyntr?

Most AI agent frameworks are libraries — you build around them. Cyntr is a **platform** — you deploy it and it runs your agents. Single Go binary. Zero external dependencies. Self-hosted.

- **29 tools, 31 skills** — agents get shell, web, cloud, Kubernetes, data analysis, and enterprise skills out of the box.
- **Multi-agent crews** — pipeline, parallel, and sequential multi-agent collaboration with delegation and orchestration.
- **MCP support** — 8 built-in Model Context Protocol servers with marketplace. Connect to the standard AI tool ecosystem.
- **Skill marketplace** — browse a built-in catalog, search GitHub, or import OpenClaw skills. Agents load skills on demand mid-conversation.
- **Slack-native** — agents respond in Slack threads with slash commands, Block Kit formatting, typing indicators, progress messages, and reaction-based approvals.
- **Cloud & Kubernetes ops** — agents run AWS/Azure/GCP CLI commands and read-only `kubectl` operations with policy enforcement.
- **Enterprise security** — PII detection/redaction, secret masking, OIDC/SSO, policy engine, SHA-256 audit hash chains, data retention.
- **Agent evaluation** — test agents with expected output matching (contains/exact/regex) and tool usage validation.
- **Token & cost tracking** — per-agent, per-provider usage with input/output token counts.
- **No vendor lock-in** — 7 LLM providers. Switch models without changing agent code.

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

### 28 Agent Tools

| Category | Tools |
|----------|-------|
| **System** | `shell_exec` (bash, 120s timeout), `code_interpreter` (Python/JS) |
| **Files** | `file_read`, `file_write`, `file_search` |
| **Web** | `browse_web`, `advanced_browse`, `chromium_browser` (headless Chrome), `web_search` (Google), `http_request` |
| **Data** | `database_query` (SQLite + PostgreSQL, read-only), `pdf_reader`, `knowledge_search` (RAG with FTS5), `json_query` (dot-notation paths), `csv_query` (stats, filter, sort) |
| **Cloud** | `aws_cross_account` (STS AssumeRole for multi-account), `aws_cost_explorer` (spend analysis), `kubectl` (read-only Kubernetes operations) |
| **Integrations** | `github` (PRs/issues), `jira` (tickets), `generate_image` (DALL-E), `transcribe_audio` (Whisper) |
| **Messaging** | `send_message` (Slack/Teams/email proactively), `send_notification` (webhook with severity levels) |
| **Knowledge** | `runbook_search` (search runbooks from knowledge base) |
| **Orchestration** | `delegate_agent`, `orchestrate_agents` (parallel multi-agent), `skill_router` (dynamically load skills mid-conversation) |
| **Custom** | Define tools in `tools/*.yaml` — no Go code required |

### 25 Enterprise Skills

Cyntr ships with 25 embedded enterprise skills across 6 categories. Skills are loaded on demand — agents only load what they need, when they need it.

| Category | Skills |
|----------|--------|
| **DevOps & SRE** (5) | `aws-infrastructure-audit`, `incident-commander`, `deployment-checker`, `cost-optimizer`, `log-analyzer` |
| **Security** (4) | `security-audit`, `dependency-scanner`, `secret-detector`, `access-reviewer` |
| **Engineering** (5) | `code-reviewer-pro`, `test-generator`, `documentation-generator`, `refactoring-assistant`, `git-analyst` |
| **Data & Analytics** (4) | `database-analyst`, `csv-analyzer`, `api-monitor`, `report-generator` |
| **Management** (4) | `standup-reporter`, `meeting-summarizer`, `status-dashboard`, `onboarding-guide` |
| **Compliance** (3) | `compliance-checker`, `change-tracker`, `data-classifier` |

Each skill bundles a system prompt, tool permissions, and configuration. Agents activate skills via the `skill_router` tool or through the dashboard.

### Skill Marketplace

Browse, search, and install skills from multiple sources:

- **Built-in catalog** — 25 enterprise skills ready to activate
- **GitHub search** — discover community skills from public repositories
- **OpenClaw import** — install skills from the OpenClaw ecosystem
- **Dashboard UI** — install, uninstall, and configure skills from the Skills page
- **CLI** — `cyntr skill list`, `cyntr skill install`, `cyntr skill import-openclaw`

```bash
# List available skills
cyntr skill list

# Install from built-in catalog
cyntr skill install incident-commander

# Import from OpenClaw
cyntr skill import-openclaw ./path/to/skill

# Search GitHub for community skills
# (also available in the dashboard)
```

### 9 Messaging Channels

| Channel | Integration |
|---------|------------|
| **Slack** | Events API + threads + slash commands + Block Kit + reactions + file uploads + progress messages + chunking |
| **Microsoft Teams** | Bot Framework + Adaptive Cards |
| **Discord** | Bot API |
| **Telegram** | Bot API webhook |
| **WhatsApp** | Business Cloud API |
| **Email** | SMTP outbound + webhook inbound |
| **Google Chat** | Webhook adapter |
| **Webhook** | Generic HTTP POST (any platform) |

### Slack-Native Experience

- **Thread replies** — `SLACK_USE_THREADS=true` keeps conversations in threads
- **Slash commands** — `/cyntr status`, `/cyntr switch <agent>`, `/cyntr clear`
- **Rich Block Kit formatting** — structured responses with sections, fields, and actions
- **Reaction commands** — approve or deny pending actions with emoji reactions
- **File upload detection** — agents can process uploaded files
- **Typing indicator** — hourglass emoji while agent works
- **Progress messages** — "Running `shell_exec`..." sent during tool execution
- **Response chunking** — auto-splits messages over 4000 chars with `[1/N]` indicators
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
- **Kubernetes support** — `kubectl` tool for read-only cluster operations (get, describe, logs)
- **Cross-account AWS** — `aws_cross_account` uses STS AssumeRole for multi-account management
- **Cost analysis** — `aws_cost_explorer` provides spend breakdowns by service, account, and time period
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
| **Multi-API key with scopes** | Separate keys for read, agent, and admin access |
| **RBAC enforcement** | 4 built-in roles (admin, team_lead, user, auditor) with 11 permissions, enforced per HTTP method |
| **OIDC/SSO** | OpenID Connect with PKCE for enterprise single sign-on |
| **Blocking approval mode** | Human-in-the-loop with 5-minute timeout for critical operations |
| **Secret masking** | Configurable patterns — AWS keys, Slack tokens, GitHub tokens, JWTs, passwords auto-redacted |
| **Audit logging** | SHA-256 hash chains for tamper-evident logs |
| **Rate limiting** | Per-tenant token bucket on proxy gateway + per-agent rate limits |
| **Config hot-reload** | Send SIGHUP to reload configuration without restart |

### Agent Runtime

The agent runtime manages the full lifecycle of AI conversations with enterprise-grade reliability:

| Feature | Description |
|---------|-------------|
| **Session auto-summarization** | Long conversations are summarized to stay within context limits |
| **Sliding window** | Older messages are pruned while preserving conversation coherence |
| **System prompt templates** | Use `{{user}}`, `{{date}}`, `{{tenant}}`, `{{agent}}` variables in prompts |
| **Tool retry** | Exponential backoff on transient tool failures |
| **Max turns warning** | Configurable turn limit prevents runaway conversations |
| **On-demand skill loading** | Agents load skills dynamically via `skill_router` |
| **Per-agent rate limits** | Throttle individual agents independently |
| **Parallel tool execution** | Multiple tool calls execute concurrently when safe |
| **Request ID propagation** | Every request carries a trace ID through all tool calls and logs |

### 17-Page Dashboard

| Page | What it does |
|------|-------------|
| **Dashboard** | Health cards, module status, recent audit, job/skill/agent counts |
| **Agents** | Create, edit (model/prompt/tools/skills), delete, chat interface with streaming |
| **Sessions** | Browse conversation history per agent, view message details |
| **Memories** | View/delete agent long-term memories |
| **Versions** | Agent version history with rollback to any previous version |
| **Knowledge** | Upload documents for RAG search, manage knowledge base |
| **Skills** | Browse catalog, install/uninstall, GitHub search, OpenClaw import, marketplace |
| **Workflows** | Register multi-step workflows, run, view step-by-step progress |
| **Scheduler** | Create cron jobs with channel delivery, view LastRun/NextRun |
| **Audit** | Filter by tenant/user/action/agent/date range, CSV export |
| **Policies** | View loaded rules, test policy decisions |
| **Approvals** | Review pending approvals, approve/deny with context |
| **Channels** | Active adapter status, configuration guide |
| **Users** | User management with API key generation and role assignment |
| **MCP Servers** | Manage Model Context Protocol servers, browse marketplace |
| **Crews** | Create and run multi-agent crews (pipeline/parallel/sequential) |
| **Eval** | Run agent evaluations with test cases, view pass rates |
| **Metrics** | Usage, token counts, request latency, error rates |
| **Federation** | Add/remove peer nodes, sync status |

### Multi-Agent Crews

Define teams of agents that collaborate on complex tasks:

```json
{
  "name": "incident-team",
  "mode": "pipeline",
  "tenant": "ops",
  "members": [
    {"agent": "monitor", "role": "detector", "goal": "Identify the issue"},
    {"agent": "cloud-ops", "role": "investigator", "goal": "Diagnose root cause"},
    {"agent": "writer", "role": "reporter", "goal": "Write incident report"}
  ]
}
```

Modes: **pipeline** (output chains to next agent), **parallel** (all agents work simultaneously), **sequential** (ordered execution with aggregated results).

### Agent Evaluation Framework

Test agents before deploying with structured test cases:

```json
{
  "agent": "assistant", "tenant": "ops",
  "cases": [
    {"input": "What is 2+2?", "expected_output": "4", "match_mode": "contains"},
    {"input": "List S3 buckets", "expected_tools": ["aws"]}
  ]
}
```

Returns pass rates, per-case scores, actual outputs, and match details.

### Model Context Protocol (MCP)

Connect to the standard AI tool ecosystem with 8 built-in MCP servers:

| Server | Purpose | Requirements |
|--------|---------|-------------|
| **filesystem** | File operations | None |
| **github** | GitHub integration | `GITHUB_PERSONAL_ACCESS_TOKEN` |
| **postgres** | Database queries | `DATABASE_URL` |
| **slack** | Slack messaging | `SLACK_BOT_TOKEN` |
| **brave-search** | Web search | `BRAVE_API_KEY` |
| **memory** | Knowledge graph | None |
| **sqlite** | SQLite queries | None |
| **puppeteer** | Browser automation | None |

Install community MCP servers from the marketplace or add custom servers via API.

### PII Detection & Data Retention

| Feature | Description |
|---------|-------------|
| **PII redaction** | SSN, credit cards, emails, phone numbers, IP addresses, dates of birth — auto-redacted in responses and history |
| **Secret masking** | AWS keys, Slack/GitHub tokens, JWTs, passwords — masked in responses, session history, and memories |
| **Data retention** | Configurable TTLs: sessions (90d), memories (180d), usage records (365d) — automatic background cleanup |
| **Token tracking** | Per-agent, per-provider input/output token counts with cost tracking |

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

- **Smart document chunking** with configurable overlap for large documents
- **File type support** — `.txt`, `.md`, `.pdf` ingestion
- **Source URL tracking** — documents link back to their origin
- **Tag-based filtering** — organize and query documents by tags
- **Runbook search** — dedicated `runbook_search` tool for operations teams

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

Chain agent actions with conditions, retries, webhooks, delays, parallel steps, loops, and human input:

```json
{
  "name": "incident-response",
  "steps": [
    {"id": "detect", "type": "agent_chat", "config": {"agent": "monitor", "message": "Check error rates"}},
    {"id": "diagnose", "type": "agent_chat", "config": {"agent": "cloud-ops", "message": "Investigate: {{detect.output}}"}},
    {"id": "approve", "type": "human_input", "config": {"prompt": "Proceed with remediation?", "timeout": "10m"}},
    {"id": "remediate", "type": "parallel", "config": {"steps": ["restart-service", "clear-cache"]}},
    {"id": "notify", "type": "webhook", "config": {"url": "https://hooks.slack.com/...", "method": "POST"}}
  ]
}
```

Step types: `agent_chat`, `tool_call`, `condition`, `webhook`, `delay`, `approval`, `parallel`, `loop`, `human_input`

- **Event triggers** — workflows can be triggered by external events
- **Cron expressions** — pure Go cron parser, no external dependencies
- **Job persistence** — workflow runs and history stored in SQLite
- **Job history** — view past runs, durations, and step-by-step output

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

Every component is a **kernel module** communicating via an in-process IPC bus. Modules boot in dependency order and shut down gracefully in reverse.

- **No external databases** — SQLite for sessions, memory, audit, knowledge, workflow history
- **No message queues** — IPC bus with backpressure
- **No container runtime** — single binary deployment
- **No configuration service** — YAML files + environment variables
- **Comprehensive error logging** — 13 modules with structured log output
- **Metrics endpoint** — `/api/v1/metrics` for monitoring integration
- **Graceful shutdown** — clean resource release on SIGTERM/SIGINT
- **Duration/timing logs** — slow operation detection across all modules

---

## SDKs

### Python

```python
from cyntr import CyntrClient

# Async client with retry logic
client = CyntrClient("http://localhost:7700", api_key="cyntr_...")

# Chat with an agent
response = await client.chat("my-org", "assistant", "What's running in us-east-1?")
print(response["content"])

# Manage knowledge base
await client.ingest_knowledge("Runbook", "Steps to restart the service...", "ops")

# List skills
skills = await client.list_skills()
```

Full type hints and async/await support. Install: `pip install ./sdk/python`

### JavaScript

```javascript
const { CyntrClient } = require('@cyntr/sdk');

const client = new CyntrClient('http://localhost:7700', 'cyntr_...');

const response = await client.chat('my-org', 'assistant', 'List S3 buckets');
console.log(response.content);

// Stream responses
const stream = client.chatStream('my-org', 'assistant', 'Analyze logs');
stream.addEventListener('message', (e) => console.log(JSON.parse(e.data)));

// Manage skills
const skills = await client.listSkills();
```

TypeScript type declarations included. Install: `npm install ./sdk/js`

---

## API Reference

All endpoints return: `{"data": ..., "meta": {"request_id", "timestamp"}, "error": null}`

70+ endpoints across 15 resource groups.

### System
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/system/health` | Module health status |
| GET | `/api/v1/system/version` | Version info |
| GET | `/api/v1/metrics` | Prometheus-compatible metrics |

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
| DELETE | `/api/v1/tenants/{tid}/agents/{name}/sessions/{sid}` | Clear session |
| GET | `/api/v1/tenants/{tid}/agents/{name}/memories` | List memories |
| DELETE | `/api/v1/tenants/{tid}/agents/{name}/memories/{mid}` | Delete memory |
| GET | `/api/v1/tenants/{tid}/agents/{name}/versions` | List agent versions |
| POST | `/api/v1/tenants/{tid}/agents/{name}/rollback/{v}` | Rollback to version |

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
| GET | `/api/v1/skills/catalog` | Browse built-in skill catalog |
| POST | `/api/v1/skills` | Install skill from path or catalog |
| DELETE | `/api/v1/skills/{name}` | Uninstall skill |
| POST | `/api/v1/skills/import/openclaw` | Import OpenClaw skill |
| GET | `/api/v1/skills/search` | Search GitHub for community skills |

### Knowledge Base
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/knowledge` | List documents |
| GET | `/api/v1/knowledge/search?q=X&mode=hybrid` | Search knowledge base |
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

### MCP Servers
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/mcp/servers` | List connected servers |
| POST | `/api/v1/mcp/servers` | Add MCP server |
| DELETE | `/api/v1/mcp/servers/{name}` | Remove server |
| GET | `/api/v1/mcp/servers/{name}/tools` | List server tools |
| GET | `/api/v1/mcp/marketplace` | Browse MCP marketplace |
| POST | `/api/v1/mcp/marketplace/install` | Install from marketplace |

### Crews
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/crews` | Create crew |
| GET | `/api/v1/crews` | List crews |
| POST | `/api/v1/crews/{id}/run` | Run crew |
| GET | `/api/v1/crews/runs/{id}` | Get run status |
| GET | `/api/v1/crews/runs` | List all runs |

### Eval
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/eval/run` | Run evaluation |
| GET | `/api/v1/eval/runs/{id}` | Get eval results |
| GET | `/api/v1/eval/runs` | List eval runs |

### Usage & Metrics
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/usage` | Query usage records |
| GET | `/api/v1/usage/summary` | Aggregated usage summary |
| GET | `/api/v1/metrics` | System metrics |
| GET | `/api/v1/branding` | White-label branding config |

### Webhooks
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/webhooks/trigger/{workflow_id}` | Trigger workflow via webhook |
| POST | `/api/v1/webhooks/agent/{tenant}/{agent}` | Send message to agent via webhook |

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
SLACK_USE_THREADS=true               # Reply in threads
TEAMS_APP_ID=...                     # Microsoft Teams
TELEGRAM_BOT_TOKEN=...               # Telegram
DISCORD_BOT_TOKEN=...                # Discord
WHATSAPP_ACCESS_TOKEN=...            # WhatsApp
EMAIL_SMTP_HOST=smtp.example.com     # Email
GOOGLE_CHAT_WEBHOOK_URL=...          # Google Chat
```

**Security**:
```bash
CYNTR_API_KEY=cyntr_...              # Primary API key (auto-generated by init)
CYNTR_API_KEYS=cyntr_read:read,cyntr_agent:agent,cyntr_admin:admin  # Multi-key with scopes
CYNTR_OIDC_ISSUER=https://...        # OIDC/SSO issuer URL
CYNTR_OIDC_CLIENT_ID=...             # OIDC client ID
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
cyntr skill install <name>
cyntr skill import-openclaw ./path/to/skill

cyntr federation peers
```

---

## Development

```bash
# Build
go build -o cyntr ./cmd/cyntr

# Test (37 packages)
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
| Enterprise skills (31) | Built-in | No | No | No |
| Skill marketplace | Built-in | No | No | No |
| MCP protocol support | 8 servers | Plugin | No | No |
| Multi-agent crews | Pipeline/Parallel/Sequential | No | Yes | Yes |
| Agent evaluation framework | Built-in | No | No | No |
| PII detection & redaction | Built-in | No | No | No |
| Token & cost tracking | Built-in | Callback | No | No |
| Policy engine | Yes | No | No | No |
| Audit logging (hash chain) | Yes | No | No | No |
| Multi-tenant | Yes | No | No | No |
| RBAC + OIDC/SSO | Yes | No | No | No |
| Slack/Teams/Discord (7 channels) | Built-in | Plugin | No | No |
| Dashboard (17 pages) | Built-in | No | No | No |
| Cloud ops (AWS/Azure/GCP/K8s) | Built-in | Plugin | No | No |
| Workflow engine + approval steps | Built-in | Chain | No | No |
| Data retention policies | Built-in | No | No | No |
| Federation | Yes | No | No | No |
| SDKs (Python + JS) | Yes | Python | Python | Python |
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
