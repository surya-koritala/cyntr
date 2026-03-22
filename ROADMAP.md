# Cyntr Roadmap

> Current version: **v0.8.0** | 142 Go files | 765 tests | 29 tools | 25 skills | 16 dashboard pages | 55 API routes

---

## v0.8.1 — Bug Fixes & Polish (Next)

**Focus:** Fix known issues from v0.8.0, visual testing, dashboard polish.

| ID | Task | Type | Priority | Test Plan |
|----|------|------|----------|-----------|
| 8.1.1 | Skills show description in installed list | Bug | High | Verify skills table shows name, version, description, author |
| 8.1.2 | Dashboard visual QA on all 16 pages | QA | High | Open each page, verify data loads, forms work, empty states show |
| 8.1.3 | Agent edit modal pre-populates correctly | Bug | High | Click Edit on agent, verify model/prompt/tools/turns pre-filled |
| 8.1.4 | Chat streaming works with real LLM (not just mock) | QA | High | Connect Azure/Claude, send message, verify chunks stream |
| 8.1.5 | Slack integration re-test after all changes | QA | High | Send message in Slack, verify progress + response + no duplicates |
| 8.1.6 | Mobile dashboard QA on real phone | QA | Medium | Open on iPhone/Android, test all pages, hamburger menu |
| 8.1.7 | Fix `cyntr init` Step 6 template output format | Bug | Medium | Run init, select templates, verify JSON files are correct |
| 8.1.8 | Knowledge base file upload works end-to-end | QA | Medium | Upload .txt and .pdf, verify searchable via knowledge_search |
| 8.1.9 | Cron scheduler works with real cron expressions | QA | Medium | Create job with "0 9 * * 1-5", verify NextRun correct |
| 8.1.10 | Approval flow works end-to-end (require_approval policy → approve in dashboard) | QA | Medium | Set policy, trigger tool, verify pending in dashboard, approve, verify agent continues |

**Release criteria:** All 10 items verified. `go test ./... -race` passes. Live-tested with real LLM provider.

---

## v0.9.0 — Production Hardening

**Focus:** Make Cyntr deployable to a real team. Security, reliability, observability.

| ID | Feature | Type | Files | Test Plan |
|----|---------|------|-------|-----------|
| 9.1 | OIDC signature verification (JWKS RS256) | Security | `auth/oidc.go` | Test with Google/Okta OIDC provider, verify token signature validated |
| 9.2 | True provider-level streaming | Performance | `runtime.go`, `agents.go` | Connect Claude, verify SSE shows real token-by-token streaming |
| 9.3 | Database migrations system | Infrastructure | `modules/agent/store.go`, new `migrations/` | Add column to schema, verify migration runs on startup |
| 9.4 | Backup & restore CLI commands | Infrastructure | `cmd/cyntr/backup.go` | `cyntr backup ./backup.tar.gz` then `cyntr restore` on fresh instance |
| 9.5 | API key persistence to SQLite | Security | `auth/session.go`, `modules/agent/store.go` | Create API key, restart, verify key still works |
| 9.6 | Audit log export (JSON + CSV) | Feature | `web/api/audit.go` | `GET /api/v1/audit/export?format=csv`, download file |
| 9.7 | Health check webhook notifications | Observability | `modules/notify/`, `cmd/cyntr/main.go` | Module goes unhealthy, verify webhook fires |
| 9.8 | Request rate limiting on API | Security | `web/api/middleware.go` | Send 100 requests/sec, verify 429 after threshold |
| 9.9 | Structured error responses with codes | DX | All API handlers | Every error has code, message, and optional details |
| 9.10 | Docker image & docker-compose.yml | Deployment | `Dockerfile`, `docker-compose.yml` | `docker build`, `docker run`, verify dashboard accessible |

**Release criteria:** Deployable to a 5-person team. OIDC login works. Data survives restarts. Backups work.

---

## v1.0.0 — General Availability

**Focus:** Production-ready for enterprises. Complete feature set. Full documentation.

| ID | Feature | Type | Test Plan |
|----|---------|------|-----------|
| 1.1 | Multi-workspace Slack | Channel | Connect 2 Slack workspaces, verify messages route to correct tenant |
| 1.2 | Audit dashboard with charts | Dashboard | View bar chart of events/day, line chart of tool usage over time |
| 1.3 | Session replay with playback | Dashboard | Click replay on session, see messages appear one by one with timing |
| 1.4 | Agent versioning & rollback | Feature | Change agent config, rollback, verify previous config restored |
| 1.5 | Usage analytics (token/cost tracking) | Dashboard | View tokens used per agent, cost per provider, daily breakdown |
| 1.6 | Webhook inbound triggers | Feature | POST to webhook URL, verify agent chat triggered with payload |
| 1.7 | CLI interactive chat mode | CLI | `cyntr chat demo assistant` opens REPL with streaming and history |
| 1.8 | Plugin system (external process tools) | Architecture | Define tool as external binary, verify agent can call it |
| 1.9 | Agent playground (sandbox testing) | Dashboard | Edit prompt in playground, send test message, see result without creating agent |
| 1.10 | Comprehensive documentation site | Docs | Generated docs covering all API endpoints, tools, skills, configuration |
| 1.11 | End-to-end test suite | Testing | Automated tests that start server, create agent, chat, verify response |
| 1.12 | Performance benchmarks | Testing | Measure: requests/sec, p99 latency, concurrent agents, memory usage |

**Release criteria:** Passes security audit. Handles 50 concurrent agents. Documentation covers all features. 3+ real deployments validated.

---

## v1.1.0 — Intelligence & Scale

**Focus:** Make agents smarter and Cyntr scalable.

| ID | Feature | Type | Test Plan |
|----|---------|------|-----------|
| 1.1.1 | Conversation memory extraction | Intelligence | Agent chats, facts auto-saved to memory, recalled in next conversation |
| 1.1.2 | Semantic search for knowledge base | Intelligence | Upload docs, search by meaning not just keywords, verify relevant results |
| 1.1.3 | Agent evaluation framework | Testing | Define test cases, run evaluations, get quality scores |
| 1.1.4 | Tool usage analytics | Observability | Dashboard shows: most used tools, failure rates, avg duration per tool |
| 1.1.5 | Horizontal scaling (multi-instance) | Scale | Run 2 Cyntr instances, verify load balanced via proxy |
| 1.1.6 | PostgreSQL backend option | Scale | Switch from SQLite to Postgres for sessions/audit/knowledge |
| 1.1.7 | Webhook integrations (PagerDuty, Datadog) | Integrations | Trigger agent from PagerDuty incident, send results to Datadog |
| 1.1.8 | Custom provider support | Extensibility | Add any OpenAI-compatible API as a provider without code changes |

---

## v1.2.0 — Enterprise & Compliance

| ID | Feature | Type | Test Plan |
|----|---------|------|-----------|
| 1.2.1 | SSO with SAML support | Security | Login via Okta SAML, verify roles mapped correctly |
| 1.2.2 | Data retention policies | Compliance | Configure 90-day retention, verify old data auto-deleted |
| 1.2.3 | PII detection & redaction in conversations | Compliance | Chat contains SSN, verify redacted in audit log |
| 1.2.4 | SOC 2 compliance report generator | Compliance | Run `compliance-checker` skill, generate PDF report |
| 1.2.5 | Multi-region federation with data residency | Scale | Deploy in US + EU, verify data stays in region |
| 1.2.6 | Custom branding (white-label dashboard) | Enterprise | Change logo, colors, title via config |
| 1.2.7 | SLA monitoring & alerting | Enterprise | Define SLAs per agent, alert on breach |
| 1.2.8 | Cost allocation per tenant | Enterprise | Track token usage per tenant, generate billing report |

---

## Completed Releases

| Version | Date | Highlights |
|---------|------|-----------|
| v0.1.0 | 2026-03-19 | Initial kernel, IPC bus, basic agent runtime |
| v0.2.0 | 2026-03-19 | Policy engine, audit logging, session persistence |
| v0.3.0 | 2026-03-19 | Dashboard, REST API, CLI, 12 tools, 5 providers |
| v0.4.0 | 2026-03-21 | 17 tools, 8 providers, 9 channels, full dashboard overhaul, cloud ops |
| v0.5.0 | 2026-03-21 | 15 UX features: progress messages, chunking, API auth, secret masking, skills |
| v0.6.0 | 2026-03-21 | 50 enhancements: security, Slack, workflows, scheduler, knowledge, SDKs |
| v0.6.1 | 2026-03-22 | Skill router infrastructure, on-demand skill loading, embedded catalog |
| v0.7.0 | 2026-03-22 | 25 enterprise skills across 6 categories |
| v0.7.1 | 2026-03-22 | Audit fixes: metrics wired, event triggers firing, dashboard tools |
| v0.8.0 | 2026-03-22 | Streaming chat, agent templates, multi-user, conversation search, mobile responsive |

---

## How to Contribute

1. Pick an item from any upcoming release
2. Check if there's an existing issue on GitHub
3. If not, create one referencing the roadmap ID (e.g., "Implement 9.3 — Database migrations")
4. Submit a PR with tests
5. Tag the PR with the target release label (e.g., `v0.9.0`)

---

## Priority Legend

- **High** — Blocks usage for most users
- **Medium** — Important but has workarounds
- **Low** — Nice to have, no workaround needed

## Type Legend

- **Bug** — Something that's broken
- **QA** — Testing/verification needed
- **Feature** — New capability
- **Security** — Security-related
- **Infrastructure** — Internal architecture
- **Dashboard** — UI changes
- **DX** — Developer experience
- **Docs** — Documentation
- **Intelligence** — AI/ML improvements
- **Scale** — Performance/scalability
- **Compliance** — Regulatory requirements
