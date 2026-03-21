package main

import (
	"fmt"
	"os"
	"os/exec"
)

func runDoctor() {
	fmt.Println()
	fmt.Println("  Cyntr Doctor — Checking your setup")
	fmt.Println()

	issues := 0

	// Check cyntr.yaml
	if _, err := os.Stat("cyntr.yaml"); err != nil {
		fmt.Println("  ✗ cyntr.yaml not found — run 'cyntr init' first")
		issues++
	} else {
		fmt.Println("  ✓ cyntr.yaml found")
	}

	// Check policy.yaml
	if _, err := os.Stat("policy.yaml"); err != nil {
		fmt.Println("  ✗ policy.yaml not found — run 'cyntr init' first")
		issues++
	} else {
		fmt.Println("  ✓ policy.yaml found")
	}

	// Check .env
	if _, err := os.Stat(".env"); err != nil {
		fmt.Println("  ⚠ .env not found — no API keys configured")
	} else {
		fmt.Println("  ✓ .env found")
	}

	// Check environment variables
	envVars := map[string]string{
		"ANTHROPIC_API_KEY":    "Claude",
		"OPENAI_API_KEY":       "GPT",
		"AZURE_OPENAI_API_KEY": "Azure OpenAI",
		"GEMINI_API_KEY":       "Gemini",
		"OPENROUTER_API_KEY":   "OpenRouter",
		"OLLAMA_URL":           "Ollama",
	}

	providerFound := false
	for env, name := range envVars {
		if os.Getenv(env) != "" {
			fmt.Printf("  ✓ %s configured (%s)\n", name, env)
			providerFound = true
		}
	}
	if !providerFound {
		fmt.Println("  ⚠ No LLM provider configured — set ANTHROPIC_API_KEY or similar")
	}

	// Check Docker
	if err := exec.Command("docker", "info").Run(); err != nil {
		fmt.Println("  ⚠ Docker not available — container isolation disabled")
	} else {
		fmt.Println("  ✓ Docker available")
	}

	// Check channels
	channels := map[string]string{
		"SLACK_BOT_TOKEN":          "Slack",
		"TEAMS_APP_ID":             "Teams",
		"WHATSAPP_ACCESS_TOKEN":    "WhatsApp",
		"TELEGRAM_BOT_TOKEN":       "Telegram",
		"DISCORD_BOT_TOKEN":        "Discord",
		"GOOGLE_CHAT_WEBHOOK_URL":  "Google Chat",
	}

	channelCount := 0
	for env, name := range channels {
		if os.Getenv(env) != "" {
			fmt.Printf("  ✓ %s channel configured\n", name)
			channelCount++
		}
	}
	if channelCount == 0 {
		fmt.Println("  ⚠ No messaging channels configured")
	}

	// Check cloud CLIs
	fmt.Println()
	fmt.Println("  Cloud Infrastructure CLIs:")
	cloudCLIs := []struct{ cmd, name, help string }{
		{"aws", "AWS CLI", "Install: https://aws.amazon.com/cli/"},
		{"az", "Azure CLI", "Install: https://learn.microsoft.com/cli/azure/install-azure-cli"},
		{"gcloud", "Google Cloud SDK", "Install: https://cloud.google.com/sdk/docs/install"},
	}
	for _, cli := range cloudCLIs {
		if _, err := exec.LookPath(cli.cmd); err == nil {
			fmt.Printf("  ✓ %s found (%s)\n", cli.name, cli.cmd)
		} else {
			fmt.Printf("  - %s not found — %s\n", cli.name, cli.help)
		}
	}

	// Check cloud-ops agent config
	if _, err := os.Stat("cloud-ops-agent.json"); err == nil {
		fmt.Println("  ✓ cloud-ops-agent.json found")
	}

	fmt.Println()
	if issues > 0 {
		fmt.Printf("  %d issue(s) found. Run 'cyntr init' to fix.\n", issues)
	} else {
		fmt.Println("  All good! Run 'cyntr start' to launch.")
	}
	fmt.Println()
}
