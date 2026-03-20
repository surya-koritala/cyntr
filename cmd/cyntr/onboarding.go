package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// isFirstRun checks if this looks like a first-time setup.
func isFirstRun() bool {
	_, err1 := os.Stat("cyntr.yaml")
	_, err2 := os.Stat(".env")
	return err1 != nil && err2 != nil
}

// showWelcome displays the welcome banner.
func showWelcome() {
	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════════════════════════╗")
	fmt.Println("  ║                                                          ║")
	fmt.Println("  ║    ██████╗██╗   ██╗███╗   ██╗████████╗██████╗           ║")
	fmt.Println("  ║   ██╔════╝╚██╗ ██╔╝████╗  ██║╚══██╔══╝██╔══██╗          ║")
	fmt.Println("  ║   ██║      ╚████╔╝ ██╔██╗ ██║   ██║   ██████╔╝          ║")
	fmt.Println("  ║   ██║       ╚██╔╝  ██║╚██╗██║   ██║   ██╔══██╗          ║")
	fmt.Println("  ║   ╚██████╗   ██║   ██║ ╚████║   ██║   ██║  ██║          ║")
	fmt.Println("  ║    ╚═════╝   ╚═╝   ╚═╝  ╚═══╝   ╚═╝   ╚═╝  ╚═╝          ║")
	fmt.Println("  ║                                                          ║")
	fmt.Println("  ║       Enterprise AI Agent Platform — v" + version + "             ║")
	fmt.Println("  ║                                                          ║")
	fmt.Println("  ╚══════════════════════════════════════════════════════════╝")
	fmt.Println()
}

// showFirstRunGuide displays a guided onboarding for new users.
func showFirstRunGuide() {
	showWelcome()

	fmt.Println("  Welcome to Cyntr! Looks like this is your first time here.")
	fmt.Println()
	fmt.Println("  Let's get you set up. You have two options:")
	fmt.Println()
	fmt.Println("  ┌─────────────────────────────────────────────────────────┐")
	fmt.Println("  │                                                         │")
	fmt.Println("  │  1) Quick Start  — guided setup wizard (recommended)    │")
	fmt.Println("  │  2) Manual       — create config files yourself         │")
	fmt.Println("  │  3) Help         — show all commands                    │")
	fmt.Println("  │                                                         │")
	fmt.Println("  └─────────────────────────────────────────────────────────┘")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("  Choose (1-3): ")
	scanner.Scan()
	choice := strings.TrimSpace(scanner.Text())

	switch choice {
	case "1", "":
		runInit()
	case "2":
		showManualGuide()
	case "3":
		showHelp()
	default:
		showHelp()
	}
}

// showManualGuide shows instructions for manual setup.
func showManualGuide() {
	fmt.Println()
	fmt.Println("  ─── Manual Setup ───")
	fmt.Println()
	fmt.Println("  1. Create cyntr.yaml:")
	fmt.Println()
	fmt.Println("     version: \"1\"")
	fmt.Println("     listen:")
	fmt.Println("       address: \"127.0.0.1:8080\"")
	fmt.Println("       webui: \":7700\"")
	fmt.Println("     tenants:")
	fmt.Println("       my-org:")
	fmt.Println("         isolation: namespace")
	fmt.Println()
	fmt.Println("  2. Create policy.yaml:")
	fmt.Println()
	fmt.Println("     rules:")
	fmt.Println("       - name: allow-all")
	fmt.Println("         tenant: \"*\"")
	fmt.Println("         action: \"*\"")
	fmt.Println("         tool: \"*\"")
	fmt.Println("         agent: \"*\"")
	fmt.Println("         decision: allow")
	fmt.Println("         priority: 1")
	fmt.Println()
	fmt.Println("  3. Set your API key:")
	fmt.Println()
	fmt.Println("     export ANTHROPIC_API_KEY=sk-ant-...")
	fmt.Println()
	fmt.Println("  4. Start:")
	fmt.Println()
	fmt.Println("     cyntr start")
	fmt.Println()
}

// showHelp displays the full command reference.
func showHelp() {
	showWelcome()

	fmt.Println("  COMMANDS")
	fmt.Println()
	fmt.Println("  Getting Started:")
	fmt.Println("    cyntr init              Interactive setup wizard")
	fmt.Println("    cyntr doctor            Check configuration and dependencies")
	fmt.Println("    cyntr start [config]    Start the Cyntr server")
	fmt.Println("    cyntr status            Show server health")
	fmt.Println("    cyntr version           Show version info")
	fmt.Println()
	fmt.Println("  Agents:")
	fmt.Println("    cyntr agent create <tenant> <name> --model <provider>")
	fmt.Println("    cyntr agent chat <tenant> <name> <message>")
	fmt.Println()
	fmt.Println("  Management:")
	fmt.Println("    cyntr tenant list           List all tenants")
	fmt.Println("    cyntr audit query            Query audit logs")
	fmt.Println("    cyntr policy test            Test a policy rule")
	fmt.Println("    cyntr skill list             List installed skills")
	fmt.Println("    cyntr skill import-openclaw  Import OpenClaw skill")
	fmt.Println("    cyntr federation peers       List federation peers")
	fmt.Println()
	fmt.Println("  ENVIRONMENT VARIABLES")
	fmt.Println()
	fmt.Println("    ANTHROPIC_API_KEY      Claude API key")
	fmt.Println("    OPENAI_API_KEY         GPT API key")
	fmt.Println("    GEMINI_API_KEY         Gemini API key")
	fmt.Println("    OLLAMA_URL             Ollama server URL")
	fmt.Println()
	fmt.Println("    SLACK_BOT_TOKEN        Enable Slack channel")
	fmt.Println("    TEAMS_APP_ID           Enable Teams channel")
	fmt.Println("    TELEGRAM_BOT_TOKEN     Enable Telegram channel")
	fmt.Println("    DISCORD_BOT_TOKEN      Enable Discord channel")
	fmt.Println("    WHATSAPP_ACCESS_TOKEN  Enable WhatsApp channel")
	fmt.Println("    EMAIL_SMTP_HOST        Enable Email channel")
	fmt.Println()
	fmt.Println("    CYNTR_API_URL          API base URL (default: http://localhost:7700)")
	fmt.Println()
	fmt.Println("  EXAMPLES")
	fmt.Println()
	fmt.Println("    # First time setup")
	fmt.Println("    cyntr init")
	fmt.Println()
	fmt.Println("    # Start with Claude")
	fmt.Println("    ANTHROPIC_API_KEY=sk-... cyntr start")
	fmt.Println()
	fmt.Println("    # Create and chat with an agent")
	fmt.Println("    cyntr agent create my-org assistant --model claude")
	fmt.Println("    cyntr agent chat my-org assistant \"What can you help with?\"")
	fmt.Println()
	fmt.Println("    # Query audit logs for a tenant")
	fmt.Println("    cyntr audit query --tenant finance")
	fmt.Println()
	fmt.Println("  DOCUMENTATION")
	fmt.Println()
	fmt.Println("    https://github.com/surya-koritala/cyntr")
	fmt.Println()
}
