package main

import (
	"bufio"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func runInit() {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println()
	fmt.Println("  ┌───────────────────────────────────────┐")
	fmt.Println("  │                                       │")
	fmt.Println("  │   Cyntr Setup Wizard                  │")
	fmt.Println("  │   Enterprise AI Agent Platform        │")
	fmt.Println("  │                                       │")
	fmt.Println("  └───────────────────────────────────────┘")
	fmt.Println()

	// Check if config already exists
	if _, err := os.Stat("cyntr.yaml"); err == nil {
		fmt.Print("  Config already exists. Overwrite? (y/N): ")
		scanner.Scan()
		if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
			fmt.Println("  Aborted.")
			return
		}
		fmt.Println()
	}

	// Step 1
	fmt.Println("  Step 1 of 6: Basic Configuration")
	fmt.Println("  ─────────────────────────────────")
	fmt.Println()
	tenantName := prompt(scanner, "  Organization/team name", "default")
	listenAddr := prompt(scanner, "  API address", "127.0.0.1:8080")
	dashboardPort := prompt(scanner, "  Dashboard port", "7700")

	// Step 2
	fmt.Println()
	fmt.Println("  Step 2 of 6: AI Model Provider")
	fmt.Println("  ──────────────────────────────")
	fmt.Println()
	fmt.Println("  1) Anthropic (Claude)  — recommended")
	fmt.Println("  2) OpenAI (GPT)")
	fmt.Println("  3) Azure OpenAI (Azure AI Foundry)")
	fmt.Println("  4) Google (Gemini)")
	fmt.Println("  5) OpenRouter (100+ models)")
	fmt.Println("  6) Ollama (local models)")
	fmt.Println("  7) Skip for now")
	fmt.Println()

	providerChoice := prompt(scanner, "  Choose", "1")

	var envLines []string
	agentModelName := "mock" // provider name used for agent configs

	switch providerChoice {
	case "1":
		key := prompt(scanner, "  Anthropic API key", "")
		if key != "" {
			envLines = append(envLines, "ANTHROPIC_API_KEY="+key)
			model := prompt(scanner, "  Model", "claude-sonnet-4-20250514")
			if model != "" {
				envLines = append(envLines, "ANTHROPIC_MODEL="+model)
			}
			agentModelName = "claude"
		}
	case "2":
		key := prompt(scanner, "  OpenAI API key", "")
		if key != "" {
			envLines = append(envLines, "OPENAI_API_KEY="+key)
			model := prompt(scanner, "  Model", "gpt-4")
			if model != "" {
				envLines = append(envLines, "OPENAI_MODEL="+model)
			}
			agentModelName = "gpt"
		}
	case "3":
		fmt.Println()
		fmt.Println("    Find these in Azure AI Foundry → your deployment → Consume tab")
		fmt.Println("    Endpoint should be the BASE URL only, e.g.:")
		fmt.Println("      https://myresource.openai.azure.com")
		fmt.Println("      https://myresource.cognitiveservices.azure.com")
		fmt.Println("    Do NOT include /openai/... path or ?api-version query string.")
		fmt.Println()
		key := prompt(scanner, "  Azure API key", "")
		if key != "" {
			envLines = append(envLines, "AZURE_OPENAI_API_KEY="+key)
			endpoint := prompt(scanner, "  Endpoint (base URL only)", "")
			// Strip any path/query the user may have pasted
			if idx := strings.Index(endpoint, "/openai"); idx > 0 {
				endpoint = endpoint[:idx]
			}
			if idx := strings.Index(endpoint, "?"); idx > 0 {
				endpoint = endpoint[:idx]
			}
			endpoint = strings.TrimRight(endpoint, "/")
			envLines = append(envLines, "AZURE_OPENAI_ENDPOINT="+endpoint)
			deployment := prompt(scanner, "  Deployment name", "gpt-4o")
			envLines = append(envLines, "AZURE_OPENAI_DEPLOYMENT="+deployment)
			apiVersion := prompt(scanner, "  API version", "2024-08-01-preview")
			envLines = append(envLines, "AZURE_OPENAI_API_VERSION="+apiVersion)
			agentModelName = "azure-openai"
		}
	case "4":
		key := prompt(scanner, "  Gemini API key", "")
		if key != "" {
			envLines = append(envLines, "GEMINI_API_KEY="+key)
			model := prompt(scanner, "  Model", "gemini-pro")
			if model != "" {
				envLines = append(envLines, "GEMINI_MODEL="+model)
			}
			agentModelName = "gemini"
		}
	case "5":
		key := prompt(scanner, "  OpenRouter API key", "")
		if key != "" {
			envLines = append(envLines, "OPENROUTER_API_KEY="+key)
			model := prompt(scanner, "  Model", "anthropic/claude-3.5-sonnet")
			if model != "" {
				envLines = append(envLines, "OPENROUTER_MODEL="+model)
			}
			agentModelName = "openrouter"
		}
	case "6":
		url := prompt(scanner, "  Ollama URL", "http://localhost:11434")
		envLines = append(envLines, "OLLAMA_URL="+url)
		model := prompt(scanner, "  Model", "llama3")
		envLines = append(envLines, "OLLAMA_MODEL="+model)
		agentModelName = "ollama"
	}

	// Step 3
	fmt.Println()
	fmt.Println("  Step 3 of 6: Messaging Channels (optional)")
	fmt.Println("  ───────────────────────────────────────────")
	fmt.Println()

	if promptYN(scanner, "  Enable Slack?") {
		token := prompt(scanner, "    Bot token (xoxb-...)", "")
		if token != "" {
			envLines = append(envLines, "SLACK_BOT_TOKEN="+token)
			envLines = append(envLines, "SLACK_TENANT="+tenantName)
			slackAgent := prompt(scanner, "    Agent to handle Slack messages", "assistant")
			envLines = append(envLines, "SLACK_AGENT="+slackAgent)
			fmt.Println()
			fmt.Println("    Note: Slack must reach your Cyntr instance.")
			fmt.Println("    For local dev, use ngrok: ngrok http 3000")
			fmt.Println("    Then set your Slack app's Event Subscription URL to:")
			fmt.Println("      https://<ngrok-url>/slack/events")
		}
	}

	if promptYN(scanner, "  Enable Teams?") {
		appID := prompt(scanner, "    App ID", "")
		if appID != "" {
			envLines = append(envLines, "TEAMS_APP_ID="+appID)
			secret := prompt(scanner, "    App Secret", "")
			envLines = append(envLines, "TEAMS_APP_SECRET="+secret)
			envLines = append(envLines, "TEAMS_TENANT="+tenantName)
		}
	}

	if promptYN(scanner, "  Enable WhatsApp?") {
		token := prompt(scanner, "    Access token", "")
		if token != "" {
			envLines = append(envLines, "WHATSAPP_ACCESS_TOKEN="+token)
			phoneID := prompt(scanner, "    Phone number ID", "")
			envLines = append(envLines, "WHATSAPP_PHONE_NUMBER_ID="+phoneID)
			envLines = append(envLines, "WHATSAPP_VERIFY_TOKEN=cyntr-verify")
			envLines = append(envLines, "WHATSAPP_TENANT="+tenantName)
		}
	}

	if promptYN(scanner, "  Enable Telegram?") {
		token := prompt(scanner, "    Bot token", "")
		if token != "" {
			envLines = append(envLines, "TELEGRAM_BOT_TOKEN="+token)
			envLines = append(envLines, "TELEGRAM_TENANT="+tenantName)
		}
	}

	if promptYN(scanner, "  Enable Discord?") {
		token := prompt(scanner, "    Bot token", "")
		if token != "" {
			envLines = append(envLines, "DISCORD_BOT_TOKEN="+token)
			envLines = append(envLines, "DISCORD_TENANT="+tenantName)
		}
	}

	if promptYN(scanner, "  Enable Google Chat?") {
		webhook := prompt(scanner, "    Webhook URL", "")
		if webhook != "" {
			envLines = append(envLines, "GOOGLE_CHAT_WEBHOOK_URL="+webhook)
			envLines = append(envLines, "GOOGLE_CHAT_TENANT="+tenantName)
			envLines = append(envLines, "GOOGLE_CHAT_AGENT=assistant")
		}
	}

	// Step 4
	fmt.Println()
	fmt.Println("  Step 4 of 6: Cloud Infrastructure Access (optional)")
	fmt.Println("  ───────────────────────────────────────────────────")
	fmt.Println()
	fmt.Println("  Cyntr agents can use CLI tools to troubleshoot cloud infrastructure.")
	fmt.Println("  This creates a read-only cloud-ops agent with shell access.")
	fmt.Println()
	fmt.Println("  Prerequisites:")
	fmt.Println("    AWS:   'aws' CLI installed + 'aws configure' done (use a read-only IAM role)")
	fmt.Println("    Azure: 'az' CLI installed + 'az login' done (use a Reader role)")
	fmt.Println("    GCP:   'gcloud' CLI installed + 'gcloud auth login' done (use Viewer role)")
	fmt.Println()

	enableCloudOps := false
	var cloudProviders []string

	if promptYN(scanner, "  Enable cloud-ops agent?") {
		enableCloudOps = true
		fmt.Println()
		if promptYN(scanner, "    AWS access? (requires 'aws' CLI configured)") {
			cloudProviders = append(cloudProviders, "aws")
		}
		if promptYN(scanner, "    Azure access? (requires 'az' CLI configured)") {
			cloudProviders = append(cloudProviders, "azure")
		}
		if promptYN(scanner, "    GCP access? (requires 'gcloud' CLI configured)") {
			cloudProviders = append(cloudProviders, "gcp")
		}
	}

	// Step 5
	fmt.Println()
	fmt.Println("  Step 5 of 6: Security Policy")
	fmt.Println("  ────────────────────────────")
	fmt.Println()
	fmt.Println("  Shell access policy for agents:")
	fmt.Println()
	fmt.Println("  1) Deny all shell access (most secure)")
	fmt.Println("  2) Require human approval for shell commands (recommended)")
	fmt.Println("  3) Allow shell for cloud-ops agent only")
	fmt.Println("  4) Allow all shell access (least secure)")
	fmt.Println()

	shellPolicy := prompt(scanner, "  Choose", "2")

	// Step 6 of 6: Agent Templates
	fmt.Println()
	fmt.Println("  Step 6 of 6: Agent Templates (optional)")
	fmt.Println("  ─────────────────────────────────────────")
	fmt.Println()
	fmt.Println("  Pre-configured agents ready to deploy:")
	fmt.Println()
	fmt.Println("  1) Cloud Ops         — AWS/Azure/GCP infrastructure troubleshooting")
	fmt.Println("  2) Code Reviewer     — PR review, bug detection, best practices")
	fmt.Println("  3) Security Scanner  — vulnerability scanning, secret detection")
	fmt.Println("  4) General Assistant — all-purpose with all tools and skills")
	fmt.Println("  5) All of the above")
	fmt.Println("  6) Skip for now")
	fmt.Println()

	templateChoice := prompt(scanner, "  Choose", "4")

	type agentTemplate struct {
		Name         string   `json:"name"`
		Tenant       string   `json:"tenant"`
		Model        string   `json:"model"`
		SystemPrompt string   `json:"system_prompt"`
		Tools        []string `json:"tools"`
		Skills       []string `json:"skills"`
		MaxTurns     int      `json:"max_turns"`
	}

	templates := map[string]agentTemplate{
		"cloud-ops": {
			Name: "cloud-ops", Model: agentModelName, MaxTurns: 20,
			Tools:        []string{"shell_exec", "http_request", "web_search", "file_read", "aws_cross_account", "aws_cost_explorer", "kubectl", "send_notification"},
			Skills:       []string{"aws-infrastructure-audit", "incident-commander", "cost-optimizer", "log-analyzer", "deployment-checker"},
			SystemPrompt: "You are a cloud infrastructure agent with direct CLI access. Run commands immediately, don't ask for permission. ONLY use read-only commands. Never modify resources.",
		},
		"code-reviewer": {
			Name: "code-reviewer", Model: agentModelName, MaxTurns: 15,
			Tools:        []string{"file_read", "file_search", "shell_exec", "github"},
			Skills:       []string{"code-reviewer-pro", "test-generator", "documentation-generator", "git-analyst"},
			SystemPrompt: "You are an expert code reviewer. Analyze code for bugs, security issues, performance problems, and style. Provide specific, actionable feedback with line numbers.",
		},
		"security-scanner": {
			Name: "security-scanner", Model: agentModelName, MaxTurns: 15,
			Tools:        []string{"shell_exec", "file_read", "file_search", "http_request"},
			Skills:       []string{"security-audit", "dependency-scanner", "secret-detector", "access-reviewer", "compliance-checker"},
			SystemPrompt: "You are a security auditing agent. Scan infrastructure and code for vulnerabilities. Generate severity-classified findings reports.",
		},
		"assistant": {
			Name: "assistant", Model: agentModelName, MaxTurns: 20,
			Tools:        []string{"*"},
			Skills:       []string{},
			SystemPrompt: "You are a helpful AI assistant with access to all tools. Execute commands directly when asked. Be concise and actionable.",
		},
	}

	var selectedTemplates []string
	switch templateChoice {
	case "1":
		selectedTemplates = []string{"cloud-ops"}
	case "2":
		selectedTemplates = []string{"code-reviewer"}
	case "3":
		selectedTemplates = []string{"security-scanner"}
	case "4":
		selectedTemplates = []string{"assistant"}
	case "5":
		selectedTemplates = []string{"cloud-ops", "code-reviewer", "security-scanner", "assistant"}
	}

	for _, name := range selectedTemplates {
		tmpl := templates[name]
		tmpl.Tenant = tenantName
		data, _ := json.Marshal(tmpl)
		filename := name + "-agent.json"
		os.WriteFile(filename, data, 0644)
		fmt.Printf("  ✓ %s\n", filename)
	}

	// Generate API key for dashboard/API access
	keyBuf := make([]byte, 32)
	crand.Read(keyBuf)
	apiKey := "cyntr_" + hex.EncodeToString(keyBuf)
	envLines = append(envLines, "CYNTR_API_KEY="+apiKey)

	// Generate files
	fmt.Println()
	fmt.Println("  Generating configuration...")
	fmt.Println()

	// cyntr.yaml
	// Quote tenant name if it contains spaces
	yamlTenantKey := tenantName
	if strings.Contains(tenantName, " ") {
		yamlTenantKey = `"` + tenantName + `"`
	}

	cyntrYAML := fmt.Sprintf(`version: "1"
listen:
  address: "%s"
  webui: ":%s"
tenants:
  %s:
    isolation: namespace
    policy: default
`, listenAddr, dashboardPort, yamlTenantKey)

	os.WriteFile("cyntr.yaml", []byte(cyntrYAML), 0644)
	fmt.Println("  ✓ cyntr.yaml")

	// policy.yaml — generate based on shell policy choice
	var shellRule string
	switch shellPolicy {
	case "1":
		shellRule = `  - name: deny-shell
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 20`
	case "3":
		shellRule = `  - name: deny-shell-global
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

  - name: deny-write-cloudops
    tenant: "*"
    action: tool_call
    tool: file_write
    agent: "cloud-ops"
    decision: deny
    priority: 30`
	case "4":
		shellRule = `  - name: allow-shell
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: allow
    priority: 20`
	default: // "2" — require approval (recommended)
		shellRule = `  - name: require-approval-shell
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: require_approval
    priority: 20`
	}

	policyYAML := fmt.Sprintf(`rules:
  - name: allow-model-calls
    tenant: "*"
    action: model_call
    tool: "*"
    agent: "*"
    decision: allow
    priority: 10

  - name: allow-tools
    tenant: "*"
    action: tool_call
    tool: "*"
    agent: "*"
    decision: allow
    priority: 5

%s

  - name: allow-default
    tenant: "*"
    action: "*"
    tool: "*"
    agent: "*"
    decision: allow
    priority: 1
`, shellRule)

	os.WriteFile("policy.yaml", []byte(policyYAML), 0644)
	fmt.Println("  ✓ policy.yaml")

	// .env — quote values to handle spaces
	if len(envLines) > 0 {
		var quotedLines []string
		for _, line := range envLines {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 && strings.ContainsAny(parts[1], " \t&?") {
				quotedLines = append(quotedLines, parts[0]+"='"+parts[1]+"'")
			} else {
				quotedLines = append(quotedLines, line)
			}
		}
		os.WriteFile(".env", []byte(strings.Join(quotedLines, "\n")+"\n"), 0600)
		fmt.Println("  ✓ .env")
	}

	// cloud-ops agent config
	if enableCloudOps && len(cloudProviders) > 0 {
		cloudList := strings.Join(cloudProviders, ", ")
		sysPrompt := fmt.Sprintf(`You are a read-only cloud infrastructure troubleshooting agent.
You have access to these cloud providers: %s.

SECURITY RULES — follow these strictly:
- ONLY use read/describe/list/get/show commands
- NEVER create, modify, delete, or update any resources
- NEVER run destructive commands (rm, delete, terminate, stop, reboot)
- If asked to make changes, explain what should be done but DO NOT execute it
- Always show the command you're about to run and explain what it does

Useful commands by provider:
`, cloudList)

		for _, cp := range cloudProviders {
			switch cp {
			case "aws":
				sysPrompt += `
AWS:
  aws sts get-caller-identity
  aws ec2 describe-instances --region <region>
  aws ecs list-clusters / describe-services
  aws logs filter-log-events --log-group-name <group>
  aws cloudwatch get-metric-statistics
  aws s3 ls
  aws rds describe-db-instances
  aws lambda list-functions
`
			case "azure":
				sysPrompt += `
Azure:
  az account show
  az vm list --output table
  az webapp list --output table
  az monitor activity-log list --max-events 20
  az aks list --output table
  az sql server list
  az storage account list
  az functionapp list
`
			case "gcp":
				sysPrompt += `
GCP:
  gcloud config list
  gcloud compute instances list
  gcloud container clusters list
  gcloud logging read "severity>=ERROR" --limit=50
  gcloud run services list
  gcloud sql instances list
  gcloud functions list
`
			}
		}

		agentJSON := fmt.Sprintf(`{
  "name": "cloud-ops",
  "tenant": "%s",
  "model": "%s",
  "system_prompt": %q,
  "tools": ["shell_exec", "http_request", "web_search", "file_read", "file_search"],
  "max_turns": 10
}
`, tenantName, agentModelName, sysPrompt)

		os.WriteFile("cloud-ops-agent.json", []byte(agentJSON), 0644)
		fmt.Println("  ✓ cloud-ops-agent.json")
		fmt.Printf("  ✓ Cloud providers: %s\n", cloudList)
	}

	// Done
	fmt.Println()
	fmt.Println("  ┌───────────────────────────────────────┐")
	fmt.Println("  │                                       │")
	fmt.Println("  │   Setup complete!                     │")
	fmt.Println("  │                                       │")
	fmt.Println("  │   To start Cyntr:                     │")
	fmt.Println("  │                                       │")
	if len(envLines) > 0 {
		fmt.Println("  │     set -a && source .env && set +a   │")
	}
	fmt.Println("  │     cyntr start                       │")
	fmt.Println("  │                                       │")
	fmt.Printf("  │   Dashboard: http://localhost:%-8s│\n", dashboardPort)
	fmt.Println("  │                                       │")
	fmt.Printf("  │   API Key: %.28s...  │\n", apiKey)
	fmt.Println("  │                                       │")
	fmt.Println("  └───────────────────────────────────────┘")

	if enableCloudOps && len(cloudProviders) > 0 {
		fmt.Println()
		fmt.Println("  ─── Cloud Ops Agent ───")
		fmt.Println()
		fmt.Println("  After starting, register the cloud-ops agent:")
		fmt.Println()
		fmt.Println("    curl -X POST localhost:7700/api/v1/tenants/" + tenantName + "/agents \\")
		fmt.Println("      -H 'Content-Type: application/json' \\")
		fmt.Println("      -d @cloud-ops-agent.json")
		fmt.Println()
		fmt.Println("  Then chat with it:")
		fmt.Println()
		fmt.Println("    cyntr agent chat " + tenantName + " cloud-ops \"Check EC2 instance health in us-east-1\"")
		fmt.Println()
		fmt.Println("  Or use the dashboard chat interface at http://localhost:" + dashboardPort)
		fmt.Println()
		fmt.Println("  Security: The agent is read-only by system prompt and policy.")
		fmt.Println("  For extra safety, use a read-only IAM role / service principal.")
	}

	fmt.Println()
}

func prompt(scanner *bufio.Scanner, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}
	scanner.Scan()
	val := strings.TrimSpace(scanner.Text())
	if val == "" {
		return defaultVal
	}
	return val
}

func promptYN(scanner *bufio.Scanner, label string) bool {
	fmt.Printf("%s (y/N): ", label)
	scanner.Scan()
	return strings.ToLower(strings.TrimSpace(scanner.Text())) == "y"
}
