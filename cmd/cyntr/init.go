package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func runInit() {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println()
	fmt.Println("  ╔═══════════════════════════════════════╗")
	fmt.Println("  ║     Cyntr — Enterprise AI Platform     ║")
	fmt.Println("  ║            Setup Wizard                 ║")
	fmt.Println("  ╚═══════════════════════════════════════╝")
	fmt.Println()

	// Check if config already exists
	if _, err := os.Stat("cyntr.yaml"); err == nil {
		fmt.Print("  cyntr.yaml already exists. Overwrite? (y/N): ")
		scanner.Scan()
		if strings.ToLower(strings.TrimSpace(scanner.Text())) != "y" {
			fmt.Println("  Aborted.")
			return
		}
	}

	// Step 1: Basic config
	fmt.Println("  ─── Step 1: Basic Configuration ───")
	fmt.Println()

	tenantName := prompt(scanner, "  Tenant name (your org/team)", "default")
	listenAddr := prompt(scanner, "  API listen address", "127.0.0.1:8080")
	dashboardPort := prompt(scanner, "  Dashboard port", "7700")

	// Step 2: LLM Provider
	fmt.Println()
	fmt.Println("  ─── Step 2: AI Model Provider ───")
	fmt.Println()
	fmt.Println("  Which LLM provider would you like to use?")
	fmt.Println("  1) Anthropic (Claude) — recommended")
	fmt.Println("  2) OpenAI (GPT)")
	fmt.Println("  3) Google (Gemini)")
	fmt.Println("  4) Ollama (local)")
	fmt.Println("  5) Skip for now")
	fmt.Println()

	providerChoice := prompt(scanner, "  Choose (1-5)", "1")

	var envLines []string

	switch providerChoice {
	case "1":
		key := prompt(scanner, "  Anthropic API key", "")
		if key != "" {
			envLines = append(envLines, "ANTHROPIC_API_KEY="+key)
			model := prompt(scanner, "  Model", "claude-sonnet-4-20250514")
			if model != "" {
				envLines = append(envLines, "ANTHROPIC_MODEL="+model)
			}
		}
	case "2":
		key := prompt(scanner, "  OpenAI API key", "")
		if key != "" {
			envLines = append(envLines, "OPENAI_API_KEY="+key)
			model := prompt(scanner, "  Model", "gpt-4")
			if model != "" {
				envLines = append(envLines, "OPENAI_MODEL="+model)
			}
		}
	case "3":
		key := prompt(scanner, "  Gemini API key", "")
		if key != "" {
			envLines = append(envLines, "GEMINI_API_KEY="+key)
			model := prompt(scanner, "  Model", "gemini-pro")
			if model != "" {
				envLines = append(envLines, "GEMINI_MODEL="+model)
			}
		}
	case "4":
		url := prompt(scanner, "  Ollama URL", "http://localhost:11434")
		envLines = append(envLines, "OLLAMA_URL="+url)
		model := prompt(scanner, "  Model", "llama3")
		envLines = append(envLines, "OLLAMA_MODEL="+model)
	}

	// Step 3: Channels
	fmt.Println()
	fmt.Println("  ─── Step 3: Channel Integrations (optional) ───")
	fmt.Println()

	if promptYN(scanner, "  Enable Slack?") {
		token := prompt(scanner, "    Slack bot token (xoxb-...)", "")
		if token != "" {
			envLines = append(envLines, "SLACK_BOT_TOKEN="+token)
			envLines = append(envLines, "SLACK_TENANT="+tenantName)
			envLines = append(envLines, "SLACK_AGENT=assistant")
		}
	}

	if promptYN(scanner, "  Enable Microsoft Teams?") {
		appID := prompt(scanner, "    Teams App ID", "")
		if appID != "" {
			envLines = append(envLines, "TEAMS_APP_ID="+appID)
			secret := prompt(scanner, "    Teams App Secret", "")
			envLines = append(envLines, "TEAMS_APP_SECRET="+secret)
			envLines = append(envLines, "TEAMS_TENANT="+tenantName)
		}
	}

	if promptYN(scanner, "  Enable WhatsApp?") {
		token := prompt(scanner, "    WhatsApp access token", "")
		if token != "" {
			envLines = append(envLines, "WHATSAPP_ACCESS_TOKEN="+token)
			phoneID := prompt(scanner, "    Phone number ID", "")
			envLines = append(envLines, "WHATSAPP_PHONE_NUMBER_ID="+phoneID)
			verifyToken := prompt(scanner, "    Verify token", "cyntr-verify")
			envLines = append(envLines, "WHATSAPP_VERIFY_TOKEN="+verifyToken)
			envLines = append(envLines, "WHATSAPP_TENANT="+tenantName)
		}
	}

	if promptYN(scanner, "  Enable Telegram?") {
		token := prompt(scanner, "    Telegram bot token", "")
		if token != "" {
			envLines = append(envLines, "TELEGRAM_BOT_TOKEN="+token)
			envLines = append(envLines, "TELEGRAM_TENANT="+tenantName)
		}
	}

	if promptYN(scanner, "  Enable Discord?") {
		token := prompt(scanner, "    Discord bot token", "")
		if token != "" {
			envLines = append(envLines, "DISCORD_BOT_TOKEN="+token)
			envLines = append(envLines, "DISCORD_TENANT="+tenantName)
		}
	}

	// Step 4: Generate files
	fmt.Println()
	fmt.Println("  ─── Generating Configuration ───")
	fmt.Println()

	// cyntr.yaml
	cyntrYAML := fmt.Sprintf(`version: "1"
listen:
  address: "%s"
  webui: ":%s"
tenants:
  %s:
    isolation: namespace
    policy: default
`, listenAddr, dashboardPort, tenantName)

	if err := os.WriteFile("cyntr.yaml", []byte(cyntrYAML), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "  Error writing cyntr.yaml: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  ✓ Created cyntr.yaml")

	// policy.yaml
	policyYAML := `rules:
  - name: allow-model-calls
    tenant: "*"
    action: model_call
    tool: "*"
    agent: "*"
    decision: allow
    priority: 10

  - name: allow-http-tool
    tenant: "*"
    action: tool_call
    tool: http_request
    agent: "*"
    decision: allow
    priority: 10

  - name: allow-file-tools
    tenant: "*"
    action: tool_call
    tool: file_read
    agent: "*"
    decision: allow
    priority: 10

  - name: allow-browse
    tenant: "*"
    action: tool_call
    tool: browse_web
    agent: "*"
    decision: allow
    priority: 10

  - name: deny-shell
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: require_approval
    priority: 20

  - name: allow-default
    tenant: "*"
    action: "*"
    tool: "*"
    agent: "*"
    decision: allow
    priority: 1
`

	if err := os.WriteFile("policy.yaml", []byte(policyYAML), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "  Error writing policy.yaml: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  ✓ Created policy.yaml")

	// .env file
	if len(envLines) > 0 {
		envContent := strings.Join(envLines, "\n") + "\n"
		if err := os.WriteFile(".env", []byte(envContent), 0600); err != nil {
			fmt.Fprintf(os.Stderr, "  Error writing .env: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("  ✓ Created .env (permissions: 600)")
	}

	// Summary
	fmt.Println()
	fmt.Println("  ═══════════════════════════════════════")
	fmt.Println("  Setup complete! To start Cyntr:")
	fmt.Println()
	if len(envLines) > 0 {
		fmt.Println("    # Load environment variables")
		fmt.Println("    source .env && export $(cat .env | xargs)")
		fmt.Println()
	}
	fmt.Println("    # Start the server")
	fmt.Println("    cyntr start")
	fmt.Println()
	fmt.Printf("    Dashboard: http://localhost:%s\n", dashboardPort)
	fmt.Printf("    API:       http://%s/api/v1/\n", listenAddr)
	fmt.Println()
	fmt.Println("  Create your first agent:")
	fmt.Println()
	fmt.Printf("    curl -X POST http://localhost:%s/api/v1/tenants/%s/agents \\\n", dashboardPort, tenantName)
	fmt.Println(`      -H "Content-Type: application/json" \`)
	fmt.Println(`      -d '{"name":"assistant","model":"claude","system_prompt":"You are a helpful assistant."}'`)
	fmt.Println()
	fmt.Println("  Chat with it:")
	fmt.Println()
	fmt.Printf("    curl -X POST http://localhost:%s/api/v1/tenants/%s/agents/assistant/chat \\\n", dashboardPort, tenantName)
	fmt.Println(`      -H "Content-Type: application/json" \`)
	fmt.Println(`      -d '{"message":"Hello!"}'`)
	fmt.Println()
	fmt.Println("  Or use the CLI:")
	fmt.Println()
	fmt.Printf("    cyntr agent create %s assistant --model claude\n", tenantName)
	fmt.Printf("    cyntr agent chat %s assistant Hello!\n", tenantName)
	fmt.Println()
	fmt.Println("  ═══════════════════════════════════════")
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
