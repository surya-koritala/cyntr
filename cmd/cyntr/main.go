package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/modules/agent"
	agentproviders "github.com/cyntr-dev/cyntr/modules/agent/providers"
	agenttools "github.com/cyntr-dev/cyntr/modules/agent/tools"
	"github.com/cyntr-dev/cyntr/modules/audit"
	"github.com/cyntr-dev/cyntr/modules/channel"
	discordpkg "github.com/cyntr-dev/cyntr/modules/channel/discord"
	emailpkg "github.com/cyntr-dev/cyntr/modules/channel/email"
	slackpkg "github.com/cyntr-dev/cyntr/modules/channel/slack"
	teamspkg "github.com/cyntr-dev/cyntr/modules/channel/teams"
	telegrampkg "github.com/cyntr-dev/cyntr/modules/channel/telegram"
	whatsapppkg "github.com/cyntr-dev/cyntr/modules/channel/whatsapp"
	"github.com/cyntr-dev/cyntr/modules/federation"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/modules/proxy"
	"github.com/cyntr-dev/cyntr/modules/scheduler"
	"github.com/cyntr-dev/cyntr/modules/skill"
	"github.com/cyntr-dev/cyntr/modules/skill/compat"
	"github.com/cyntr-dev/cyntr/web"
	webapi "github.com/cyntr-dev/cyntr/web/api"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "version":
		fmt.Printf("cyntr v%s\n", version)
	case "start":
		runStart()
	case "status":
		apiGet("/api/v1/system/health")
	default:
		runCLI(os.Args[1:])
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: cyntr <command>")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  start [config]                              Start the Cyntr server")
	fmt.Fprintln(os.Stderr, "  status                                      Show server health")
	fmt.Fprintln(os.Stderr, "  version                                     Show version")
	fmt.Fprintln(os.Stderr, "  tenant list                                 List all tenants")
	fmt.Fprintln(os.Stderr, "  agent create <tenant> <name> [--model m]    Create an agent")
	fmt.Fprintln(os.Stderr, "  agent list <tenant>                         List agents for a tenant")
	fmt.Fprintln(os.Stderr, "  agent chat <tenant> <agent> <message>       Chat with an agent")
	fmt.Fprintln(os.Stderr, "  audit query [--tenant t]                    Query audit log")
	fmt.Fprintln(os.Stderr, "  policy test --tenant t --action a --tool t  Test a policy")
	fmt.Fprintln(os.Stderr, "  federation peers                            List federation peers")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "environment:")
	fmt.Fprintln(os.Stderr, "  CYNTR_API_URL   API base URL (default: http://localhost:7700)")
}

func runStart() {
	cfgPath := "cyntr.yaml"
	if len(os.Args) > 2 {
		cfgPath = os.Args[2]
	}

	k := kernel.New()

	if err := k.LoadConfig(cfgPath); err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	cfg := k.Config().Get()

	// Determine policy path from config or default
	policyPath := "policy.yaml"

	// Register all modules
	policyEngine := policy.NewEngine(policyPath)
	auditLogger := audit.NewLogger("audit.db", "cyntr-local", "audit-secret")
	agentRuntime := agent.NewRuntime()

	sessionStore, err := agent.NewSessionStore("sessions.db")
	if err != nil {
		fmt.Fprintf(os.Stderr, "session store error: %v\n", err)
		os.Exit(1)
	}
	agentRuntime.SetSessionStore(sessionStore)

	memoryStore, _ := agent.NewMemoryStore("memory.db")
	agentRuntime.SetMemoryStore(memoryStore)

	agentRuntime.RegisterProvider(agentproviders.NewMock("Default mock response"))

	// Register tools
	toolReg := agent.NewToolRegistry()
	toolReg.Register(&agenttools.ShellTool{})
	toolReg.Register(agenttools.NewHTTPTool())
	toolReg.Register(&agenttools.FileReadTool{})
	toolReg.Register(&agenttools.FileWriteTool{})
	toolReg.Register(&agenttools.FileSearchTool{})
	toolReg.Register(agenttools.NewBrowserTool())
	agentRuntime.SetToolRegistry(toolReg)

	// Register Claude provider if API key is set
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey != "" {
		claudeModel := os.Getenv("ANTHROPIC_MODEL")
		if claudeModel == "" {
			claudeModel = "claude-sonnet-4-20250514"
		}
		agentRuntime.RegisterProvider(agentproviders.NewAnthropic(anthropicKey, claudeModel, ""))
		fmt.Printf("registered Claude provider (model: %s)\n", claudeModel)
	}

	// Register OpenAI provider if API key is set
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey != "" {
		openaiModel := os.Getenv("OPENAI_MODEL")
		if openaiModel == "" {
			openaiModel = "gpt-4"
		}
		agentRuntime.RegisterProvider(agentproviders.NewOpenAI(openaiKey, openaiModel, ""))
		fmt.Printf("registered GPT provider (model: %s)\n", openaiModel)
	}

	// Register Gemini provider if API key is set
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey != "" {
		geminiModel := os.Getenv("GEMINI_MODEL")
		if geminiModel == "" {
			geminiModel = "gemini-pro"
		}
		agentRuntime.RegisterProvider(agentproviders.NewGemini(geminiKey, geminiModel, ""))
		fmt.Printf("registered Gemini provider (model: %s)\n", geminiModel)
	}

	// Register Ollama provider if URL is set
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL != "" {
		ollamaModel := os.Getenv("OLLAMA_MODEL")
		if ollamaModel == "" {
			ollamaModel = "llama3"
		}
		agentRuntime.RegisterProvider(agentproviders.NewOllama(ollamaModel, ollamaURL))
		fmt.Printf("registered Ollama provider (model: %s)\n", ollamaModel)
	}

	channelMgr := channel.NewManager()

	// Register Slack adapter if token is set
	slackToken := os.Getenv("SLACK_BOT_TOKEN")
	if slackToken != "" {
		slackTenant := os.Getenv("SLACK_TENANT")
		if slackTenant == "" {
			slackTenant = "default"
		}
		slackAgent := os.Getenv("SLACK_AGENT")
		if slackAgent == "" {
			slackAgent = "assistant"
		}
		slackAddr := os.Getenv("SLACK_LISTEN_ADDR")
		if slackAddr == "" {
			slackAddr = "127.0.0.1:3000"
		}
		slackAdapter := slackpkg.New(slackAddr, slackToken, slackTenant, slackAgent)
		channelMgr.AddAdapter(slackAdapter)
		fmt.Printf("registered Slack adapter (tenant: %s, agent: %s, listen: %s)\n", slackTenant, slackAgent, slackAddr)
	}

	// Register Teams adapter if configured
	teamsAppID := os.Getenv("TEAMS_APP_ID")
	if teamsAppID != "" {
		teamsSecret := os.Getenv("TEAMS_APP_SECRET")
		teamsTenant := os.Getenv("TEAMS_TENANT")
		if teamsTenant == "" {
			teamsTenant = "default"
		}
		teamsAgent := os.Getenv("TEAMS_AGENT")
		if teamsAgent == "" {
			teamsAgent = "assistant"
		}
		teamsAddr := os.Getenv("TEAMS_LISTEN_ADDR")
		if teamsAddr == "" {
			teamsAddr = "127.0.0.1:3001"
		}
		teamsAdapter := teamspkg.New(teamsAddr, teamsAppID, teamsSecret, teamsTenant, teamsAgent)
		channelMgr.AddAdapter(teamsAdapter)
		fmt.Printf("registered Teams adapter (tenant: %s, listen: %s)\n", teamsTenant, teamsAddr)
	}

	// Register Email adapter if configured
	emailSMTP := os.Getenv("EMAIL_SMTP_HOST")
	if emailSMTP != "" {
		emailPort := os.Getenv("EMAIL_SMTP_PORT")
		if emailPort == "" {
			emailPort = "587"
		}
		emailFrom := os.Getenv("EMAIL_FROM")
		if emailFrom == "" {
			emailFrom = "cyntr@localhost"
		}
		emailTenant := os.Getenv("EMAIL_TENANT")
		if emailTenant == "" {
			emailTenant = "default"
		}
		emailAgent := os.Getenv("EMAIL_AGENT")
		if emailAgent == "" {
			emailAgent = "assistant"
		}
		emailAddr := os.Getenv("EMAIL_LISTEN_ADDR")
		if emailAddr == "" {
			emailAddr = "127.0.0.1:3002"
		}
		emailAdapter := emailpkg.New(emailAddr, emailSMTP, emailPort, emailFrom, emailTenant, emailAgent)
		channelMgr.AddAdapter(emailAdapter)
		fmt.Printf("registered Email adapter (tenant: %s, listen: %s)\n", emailTenant, emailAddr)
	}

	// Register WhatsApp adapter if configured
	waToken := os.Getenv("WHATSAPP_ACCESS_TOKEN")
	if waToken != "" {
		waPhoneNumID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID")
		waVerifyToken := os.Getenv("WHATSAPP_VERIFY_TOKEN")
		waTenant := os.Getenv("WHATSAPP_TENANT")
		if waTenant == "" {
			waTenant = "default"
		}
		waAgent := os.Getenv("WHATSAPP_AGENT")
		if waAgent == "" {
			waAgent = "assistant"
		}
		waAddr := os.Getenv("WHATSAPP_LISTEN_ADDR")
		if waAddr == "" {
			waAddr = "127.0.0.1:3003"
		}
		waAdapter := whatsapppkg.New(waAddr, waToken, waPhoneNumID, waVerifyToken, waTenant, waAgent)
		channelMgr.AddAdapter(waAdapter)
		fmt.Printf("registered WhatsApp adapter (tenant: %s, agent: %s, listen: %s)\n", waTenant, waAgent, waAddr)
	}

	// Register Telegram adapter if configured
	tgToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if tgToken != "" {
		tgTenant := os.Getenv("TELEGRAM_TENANT")
		if tgTenant == "" {
			tgTenant = "default"
		}
		tgAgent := os.Getenv("TELEGRAM_AGENT")
		if tgAgent == "" {
			tgAgent = "assistant"
		}
		tgAddr := os.Getenv("TELEGRAM_LISTEN_ADDR")
		if tgAddr == "" {
			tgAddr = "127.0.0.1:3004"
		}
		tgAdapter := telegrampkg.New(tgAddr, tgToken, tgTenant, tgAgent)
		channelMgr.AddAdapter(tgAdapter)
		fmt.Printf("registered Telegram adapter (tenant: %s, agent: %s, listen: %s)\n", tgTenant, tgAgent, tgAddr)
	}

	// Register Discord adapter if configured
	discordToken := os.Getenv("DISCORD_BOT_TOKEN")
	if discordToken != "" {
		discordTenant := os.Getenv("DISCORD_TENANT")
		if discordTenant == "" {
			discordTenant = "default"
		}
		discordAgent := os.Getenv("DISCORD_AGENT")
		if discordAgent == "" {
			discordAgent = "assistant"
		}
		discordAddr := os.Getenv("DISCORD_LISTEN_ADDR")
		if discordAddr == "" {
			discordAddr = "127.0.0.1:3005"
		}
		discordAdapter := discordpkg.New(discordAddr, discordToken, discordTenant, discordAgent)
		channelMgr.AddAdapter(discordAdapter)
		fmt.Printf("registered Discord adapter (tenant: %s, agent: %s, listen: %s)\n", discordTenant, discordAgent, discordAddr)
	}

	proxyGateway := proxy.NewGateway(cfg.Listen.Address)
	skillRuntime := skill.NewRuntime()
	skillRuntime.SetOpenClawLoader(compat.LoadOpenClawSkillFromFile)
	federationMod := federation.NewModule("cyntr-local")
	schedulerMod := scheduler.New()

	k.Register(policyEngine)
	k.Register(auditLogger)
	k.Register(agentRuntime)
	k.Register(channelMgr)
	k.Register(proxyGateway)
	k.Register(skillRuntime)
	k.Register(federationMod)
	k.Register(schedulerMod)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := k.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "start error: %v\n", err)
		os.Exit(1)
	}

	// Start API + Dashboard server
	apiServer := webapi.NewServer(k.Bus(), k)
	dashboard := web.NewDashboardHandler()

	mux := http.NewServeMux()
	mux.Handle("/api/", apiServer)
	mux.Handle("/", dashboard)

	webAddr := cfg.Listen.WebUI
	if webAddr == "" {
		webAddr = ":7700"
	}

	go func() {
		fmt.Printf("cyntr dashboard: http://localhost%s\n", webAddr)
		if err := http.ListenAndServe(webAddr, mux); err != nil {
			fmt.Fprintf(os.Stderr, "web server error: %v\n", err)
		}
	}()

	fmt.Println("cyntr started")

	// Signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	for sig := range sigCh {
		switch sig {
		case syscall.SIGHUP:
			fmt.Println("received SIGHUP, reloading config...")
			if err := k.ReloadConfig(); err != nil {
				fmt.Fprintf(os.Stderr, "config reload error: %v\n", err)
			} else {
				fmt.Println("config reloaded")
			}
		case syscall.SIGINT, syscall.SIGTERM:
			fmt.Printf("\nreceived %s, shutting down...\n", sig)
			if err := k.Stop(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "stop error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("cyntr stopped")
			return
		}
	}
}
