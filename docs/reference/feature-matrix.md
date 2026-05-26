[Cyntr Docs](../README.md) > Reference > Feature matrix

# Feature matrix

The full list of providers, tools, skills, channels, MCP servers, and dashboard pages. Use this page when you want exhaustive coverage; the [main README](../../README.md) only mentions counts.

## LLM providers (8)

| Provider | Models | Auth |
|----------|--------|------|
| **Anthropic** | Claude 4, Sonnet, Haiku (streaming) | `ANTHROPIC_API_KEY` |
| **OpenAI** | GPT-4o, GPT-4, GPT-3.5 | `OPENAI_API_KEY` |
| **Azure OpenAI** | Azure AI Foundry deployments | `AZURE_OPENAI_API_KEY` + endpoint |
| **Google Gemini** | Gemini Pro, Flash | `GEMINI_API_KEY` |
| **OpenRouter** | 100+ models via single key | `OPENROUTER_API_KEY` |
| **Ollama** | Llama, Mistral, CodeLlama (local) | `OLLAMA_URL` |
| **Mock** | Testing and development | Always available |

## Tools (28 + custom)

| Category | Tools |
|----------|-------|
| **System** | `shell_exec` (bash, 120s timeout), `code_interpreter` (Python/JS) |
| **Files** | `file_read`, `file_write`, `file_search` |
| **Web** | `browse_web`, `advanced_browse`, `chromium_browser`, `web_search`, `http_request` |
| **Data** | `database_query` (SQLite + PostgreSQL, read-only), `pdf_reader`, `knowledge_search` (RAG with FTS5), `json_query`, `csv_query` |
| **Cloud** | `aws_cross_account` (STS AssumeRole), `aws_cost_explorer`, `kubectl` (read-only) |
| **Integrations** | `github`, `jira`, `generate_image` (DALL-E), `transcribe_audio` (Whisper) |
| **Messaging** | `send_message` (Slack/Teams/email), `send_notification` |
| **Knowledge** | `runbook_search` |
| **Orchestration** | `delegate_agent`, `orchestrate_agents`, `skill_router` |
| **User model** | `user_model_read`, `user_model_write` |
| **Custom** | Define in `tools/*.yaml`, no Go required |

Vertical packs (opt-in) add more tools — see [`packs/README.md`](../../packs/README.md).

## Skills (12+)

Curated catalog plus marketplace.

| Category | Skills |
|----------|--------|
| **First-party** | `cloud-diagnostics`, `code-review`, `incident-response`, `document-summarizer`, `data-analyst`, `customer-support`, `security-scanner`, `api-tester` |
| **OpenClaw community** | `openclaw-weather-checker`, `openclaw-code-reviewer`, `openclaw-doc-writer`, `openclaw-cyntr-security` |

Browse and install via `cyntr skill list` / `cyntr skill marketplace` / dashboard.

## Channels (9)

| Channel | Integration |
|---------|-------------|
| **Slack** | Events API + threads + slash commands + Block Kit + reactions + file uploads + progress + chunking |
| **Microsoft Teams** | Bot Framework + Adaptive Cards |
| **Discord** | Bot API |
| **Telegram** | Bot API webhook |
| **WhatsApp** | Business Cloud API |
| **Email** | SMTP outbound + webhook inbound |
| **Google Chat** | Webhook adapter |
| **Webhook** | Generic HTTP POST (any platform) |
| **Signal** | Signal-CLI bridge |

## MCP servers (8 built-in)

| Server | Purpose | Requirements |
|--------|---------|--------------|
| **filesystem** | File operations | None |
| **github** | GitHub integration | `GITHUB_PERSONAL_ACCESS_TOKEN` |
| **postgres** | Database queries | `DATABASE_URL` |
| **slack** | Slack messaging | `SLACK_BOT_TOKEN` |
| **brave-search** | Web search | `BRAVE_API_KEY` |
| **memory** | Knowledge graph | None |
| **sqlite** | SQLite queries | None |
| **puppeteer** | Browser automation | None |

Plus community MCP servers via the marketplace.

## Dashboard pages (19)

| Page | What it does |
|------|--------------|
| Dashboard | Health cards, module status, recent audit, job/skill/agent counts |
| Agents | Create, edit, delete, chat with streaming |
| Sessions | Browse conversation history |
| Memories | View/delete agent long-term memories |
| Versions | Agent version history with rollback |
| Knowledge | Upload documents for RAG, manage knowledge base |
| Skills | Browse catalog, install/uninstall, GitHub search, marketplace |
| Workflows | Register multi-step workflows, run, view step progress |
| Scheduler | Create cron jobs with channel delivery |
| Audit | Filter by tenant/user/action/agent/date, CSV export |
| Policies | View loaded rules, test policy decisions |
| Approvals | Review pending approvals, approve/deny |
| Channels | Active adapter status |
| Users | User management with API key gen, role assignment |
| MCP Servers | Manage MCP servers, marketplace |
| Crews | Create and run multi-agent crews |
| Eval | Run evaluations, view pass rates |
| Metrics | Usage, tokens, latency, error rates |
| Federation | Add/remove peer nodes, sync status |

## Comparison summary

| Feature | Cyntr | LangChain | CrewAI | AutoGen |
|---------|-------|-----------|--------|---------|
| Self-hosted platform | Yes | Library | Library | Library |
| Single binary | Yes | No | No | No |
| Enterprise skills | Built-in | No | No | No |
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
| Slack/Teams/Discord (9 channels) | Built-in | Plugin | No | No |
| Dashboard | 19 pages | None | None | None |
| Cloud ops (AWS/Azure/GCP/K8s) | Built-in | Plugin | No | No |
| Workflow engine + approval steps | Built-in | Chain | No | No |
| Data retention policies | Built-in | No | No | No |
| Federation | Yes | No | No | No |
| SDKs (Python + JS) | Yes | Python | Python | Python |
| Zero dependencies | Yes | Many | Many | Many |

## Related

- [Comparison: vs Hermes](../comparison/vs-hermes.md)
- [Comparison: vs Dify](../comparison/vs-dify.md)
- [Comparison: vs LangChain](../comparison/vs-langchain.md)
