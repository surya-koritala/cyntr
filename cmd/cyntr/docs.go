package main

import (
	"fmt"
	"os"
	"strings"
)

func runDocs(args []string) {
	outputDir := "docs"
	if len(args) > 0 {
		outputDir = args[0]
	}

	os.MkdirAll(outputDir, 0755)

	fmt.Println("Generating Cyntr documentation...")
	fmt.Println()

	// API Reference
	writeAPIReference(outputDir)

	// Tools Reference
	writeToolsReference(outputDir)

	// Skills Reference
	writeSkillsReference(outputDir)

	// CLI Reference
	writeCLIReference(outputDir)

	// Configuration Reference
	writeConfigReference(outputDir)

	fmt.Printf("\nDocumentation generated in %s/\n", outputDir)
}

func writeAPIReference(dir string) {
	content := `# Cyntr API Reference

All endpoints return: ` + "`" + `{"data": ..., "meta": {"request_id", "timestamp"}, "error": null}` + "`" + `

## System
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | /api/v1/system/health | Module health status |
| GET | /api/v1/system/version | Version info |
| GET | /api/v1/metrics | Request metrics |

## Tenants
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | /api/v1/tenants | List tenants |
| GET | /api/v1/tenants/{tid} | Get tenant |
| POST | /api/v1/tenants | Create tenant |
| DELETE | /api/v1/tenants/{tid} | Delete tenant |

## Agents
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | /api/v1/tenants/{tid}/agents | List agents |
| POST | /api/v1/tenants/{tid}/agents | Create agent |
| GET | /api/v1/tenants/{tid}/agents/{name} | Get agent config |
| PUT | /api/v1/tenants/{tid}/agents/{name} | Update agent |
| DELETE | /api/v1/tenants/{tid}/agents/{name} | Delete agent |
| POST | /api/v1/tenants/{tid}/agents/{name}/chat | Chat |
| GET | /api/v1/tenants/{tid}/agents/{name}/stream | SSE streaming |
| GET | /api/v1/tenants/{tid}/agents/{name}/sessions | List sessions |
| GET | /api/v1/tenants/{tid}/agents/{name}/sessions/{sid}/messages | Messages |
| GET | /api/v1/tenants/{tid}/agents/{name}/memories | List memories |
| DELETE | /api/v1/tenants/{tid}/agents/{name}/memories/{mid} | Delete memory |
| GET | /api/v1/tenants/{tid}/agents/{name}/versions | Version history |
| POST | /api/v1/tenants/{tid}/agents/{name}/rollback/{v} | Rollback config |

## Webhooks
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | /api/v1/webhooks/agent/{tenant}/{agent} | Trigger agent via webhook |
| POST | /api/v1/webhooks/trigger/{workflow_id} | Trigger workflow |

## Crews
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | /api/v1/crews | Create crew |
| GET | /api/v1/crews | List crews |
| POST | /api/v1/crews/{id}/run | Run crew |
| GET | /api/v1/crews/runs/{run_id} | Run status |
| GET | /api/v1/crews/runs | List runs |

## Skills, Knowledge, Workflows, Scheduler, Audit, MCP, Users, Eval
See the full API at https://github.com/surya-koritala/cyntr
`
	os.WriteFile(dir+"/api-reference.md", []byte(content), 0644)
	fmt.Println("  + api-reference.md")
}

func writeToolsReference(dir string) {
	tools := []struct{ name, desc string }{
		{"shell_exec", "Execute bash commands (120s timeout)"},
		{"http_request", "Make HTTP requests"},
		{"file_read", "Read file contents"},
		{"file_write", "Write to files"},
		{"file_search", "Search files by glob pattern"},
		{"browse_web", "Fetch and extract text from web pages"},
		{"advanced_browse", "Web scraping with CSS selectors"},
		{"chromium_browser", "Headless Chrome automation"},
		{"web_search", "Google Custom Search API"},
		{"database_query", "SQL queries (SQLite + PostgreSQL)"},
		{"pdf_reader", "Extract text from PDF files"},
		{"knowledge_search", "Search knowledge base (FTS5 + semantic)"},
		{"json_query", "Parse JSON with dot-notation paths"},
		{"csv_query", "Analyze CSV data (stats, filter, sort)"},
		{"generate_image", "DALL-E image generation"},
		{"transcribe_audio", "Speech-to-text (Whisper)"},
		{"github", "GitHub PR/issue management"},
		{"jira", "Jira ticket management"},
		{"kubectl", "Read-only Kubernetes operations"},
		{"aws_cross_account", "Multi-account AWS via STS AssumeRole"},
		{"aws_cost_explorer", "AWS spend analysis"},
		{"send_message", "Send to Slack/Teams/email channels"},
		{"send_notification", "Webhook notifications with severity"},
		{"delegate_agent", "Delegate to another agent"},
		{"orchestrate_agents", "Parallel multi-agent execution"},
		{"skill_router", "Load skills on demand"},
		{"runbook_search", "Search runbooks from knowledge base"},
		{"code_interpreter", "Execute Python/JavaScript"},
	}

	var sb strings.Builder
	sb.WriteString("# Cyntr Tools Reference\n\n")
	sb.WriteString(fmt.Sprintf("Cyntr has %d built-in tools.\n\n", len(tools)))
	sb.WriteString("| Tool | Description |\n|------|-------------|\n")
	for _, t := range tools {
		sb.WriteString(fmt.Sprintf("| `%s` | %s |\n", t.name, t.desc))
	}
	os.WriteFile(dir+"/tools-reference.md", []byte(sb.String()), 0644)
	fmt.Println("  + tools-reference.md")
}

func writeSkillsReference(dir string) {
	content := `# Cyntr Skills Reference

25 enterprise skills embedded in the binary, organized by category.

## DevOps & SRE
- aws-infrastructure-audit, incident-commander, deployment-checker, cost-optimizer, log-analyzer

## Security
- security-audit, dependency-scanner, secret-detector, access-reviewer

## Engineering
- code-reviewer-pro, test-generator, documentation-generator, refactoring-assistant, git-analyst

## Data & Analytics
- database-analyst, csv-analyzer, api-monitor, report-generator

## Management
- standup-reporter, meeting-summarizer, status-dashboard, onboarding-guide

## Compliance
- compliance-checker, change-tracker, data-classifier
`
	os.WriteFile(dir+"/skills-reference.md", []byte(content), 0644)
	fmt.Println("  + skills-reference.md")
}

func writeCLIReference(dir string) {
	content := `# Cyntr CLI Reference

## Commands

| Command | Description |
|---------|-------------|
| cyntr init | Interactive 6-step setup wizard |
| cyntr start | Start the server |
| cyntr doctor | Validate config and cloud CLI auth |
| cyntr status | Show server health |
| cyntr version | Show version |
| cyntr chat <tenant> <agent> | Interactive terminal chat |
| cyntr backup [path] | Backup databases and config |
| cyntr restore <path> | Restore from backup |
| cyntr docs [dir] | Generate documentation |
| cyntr help | Show all commands |
`
	os.WriteFile(dir+"/cli-reference.md", []byte(content), 0644)
	fmt.Println("  + cli-reference.md")
}

func writeConfigReference(dir string) {
	content := `# Cyntr Configuration Reference

## cyntr.yaml
` + "```yaml" + `
version: "1"
listen:
  address: "127.0.0.1:8080"
  webui: ":7700"
tenants:
  my-org:
    isolation: namespace
    policy: default
` + "```" + `

## Environment Variables

### LLM Providers
| Variable | Provider |
|----------|----------|
| ANTHROPIC_API_KEY | Claude |
| OPENAI_API_KEY | GPT |
| AZURE_OPENAI_API_KEY | Azure OpenAI |
| AZURE_OPENAI_ENDPOINT | Azure endpoint |
| AZURE_OPENAI_DEPLOYMENT | Azure deployment |
| GEMINI_API_KEY | Gemini |
| OPENROUTER_API_KEY | OpenRouter |
| OLLAMA_URL | Ollama |

### Channels
| Variable | Channel |
|----------|---------|
| SLACK_BOT_TOKEN | Slack |
| SLACK_ROUTES | Per-channel routing |
| SLACK_USE_THREADS | Thread replies |
| TEAMS_APP_ID | Teams |
| TELEGRAM_BOT_TOKEN | Telegram |
| DISCORD_BOT_TOKEN | Discord |
| WHATSAPP_ACCESS_TOKEN | WhatsApp |
| EMAIL_SMTP_HOST | Email |
| GOOGLE_CHAT_WEBHOOK_URL | Google Chat |

### Security
| Variable | Purpose |
|----------|---------|
| CYNTR_API_KEY | API authentication |
| MCP_SERVERS | MCP server configs (JSON) |
`
	os.WriteFile(dir+"/config-reference.md", []byte(content), 0644)
	fmt.Println("  + config-reference.md")
}
