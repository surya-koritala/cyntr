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
	fmt.Println("  Step 1 of 3: Basic Configuration")
	fmt.Println("  ─────────────────────────────────")
	fmt.Println()
	tenantName := prompt(scanner, "  Organization/team name", "default")
	listenAddr := prompt(scanner, "  API address", "127.0.0.1:8080")
	dashboardPort := prompt(scanner, "  Dashboard port", "7700")

	// Step 2
	fmt.Println()
	fmt.Println("  Step 2 of 3: AI Model Provider")
	fmt.Println("  ──────────────────────────────")
	fmt.Println()
	fmt.Println("  1) Anthropic (Claude)  — recommended")
	fmt.Println("  2) OpenAI (GPT)")
	fmt.Println("  3) Google (Gemini)")
	fmt.Println("  4) Ollama (local models)")
	fmt.Println("  5) Skip for now")
	fmt.Println()

	providerChoice := prompt(scanner, "  Choose", "1")

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

	// Step 3
	fmt.Println()
	fmt.Println("  Step 3 of 3: Messaging Channels (optional)")
	fmt.Println("  ───────────────────────────────────────────")
	fmt.Println()

	if promptYN(scanner, "  Enable Slack?") {
		token := prompt(scanner, "    Bot token (xoxb-...)", "")
		if token != "" {
			envLines = append(envLines, "SLACK_BOT_TOKEN="+token)
			envLines = append(envLines, "SLACK_TENANT="+tenantName)
			envLines = append(envLines, "SLACK_AGENT=assistant")
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

	// Generate files
	fmt.Println()
	fmt.Println("  Generating configuration...")
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

	os.WriteFile("cyntr.yaml", []byte(cyntrYAML), 0644)
	fmt.Println("  ✓ cyntr.yaml")

	// policy.yaml
	policyYAML := `rules:
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

  - name: require-approval-shell
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
	os.WriteFile("policy.yaml", []byte(policyYAML), 0644)
	fmt.Println("  ✓ policy.yaml")

	// .env
	if len(envLines) > 0 {
		os.WriteFile(".env", []byte(strings.Join(envLines, "\n")+"\n"), 0600)
		fmt.Println("  ✓ .env")
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
		fmt.Println("  │     source .env && export $(cat .env) │")
	}
	fmt.Println("  │     cyntr start                       │")
	fmt.Println("  │                                       │")
	fmt.Printf("  │   Dashboard: http://localhost:%-8s│\n", dashboardPort)
	fmt.Println("  │                                       │")
	fmt.Println("  └───────────────────────────────────────┘")
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
