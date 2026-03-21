package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/agent"
	agentproviders "github.com/cyntr-dev/cyntr/modules/agent/providers"
	agenttools "github.com/cyntr-dev/cyntr/modules/agent/tools"
	"github.com/cyntr-dev/cyntr/modules/audit"
	"github.com/cyntr-dev/cyntr/modules/channel"
	discordpkg "github.com/cyntr-dev/cyntr/modules/channel/discord"
	emailpkg "github.com/cyntr-dev/cyntr/modules/channel/email"
	googlechatpkg "github.com/cyntr-dev/cyntr/modules/channel/googlechat"
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
	"github.com/cyntr-dev/cyntr/modules/workflow"
	"github.com/cyntr-dev/cyntr/tenant"
	"github.com/cyntr-dev/cyntr/web"
	webapi "github.com/cyntr-dev/cyntr/web/api"
)

const version = "0.4.0"

func main() {
	if len(os.Args) < 2 {
		if isFirstRun() {
			showFirstRunGuide()
		} else {
			showHelp()
		}
		return
	}

	switch os.Args[1] {
	case "version":
		fmt.Printf("cyntr v%s\n", version)
	case "init":
		runInit()
	case "doctor":
		runDoctor()
	case "start":
		runStart()
	case "status":
		apiGet("/api/v1/system/health")
	case "help", "--help", "-h":
		showHelp()
	default:
		runCLI(os.Args[1:])
	}
}

func printUsage() {
	showHelp()
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

	log.Info("config loaded", map[string]any{"path": cfgPath})

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
	toolReg.Register(agenttools.NewAdvancedBrowserTool())
	toolReg.Register(agenttools.NewGitHubTool())
	toolReg.Register(agenttools.NewJiraTool())
	toolReg.Register(agenttools.NewDelegateTool(k.Bus()))
	toolReg.Register(agenttools.NewCodeInterpreterTool())
	toolReg.Register(agenttools.NewTranscribeTool())
	toolReg.Register(agenttools.NewWebSearchTool())
	toolReg.Register(agenttools.NewPDFReaderTool())
	toolReg.Register(agenttools.NewDatabaseTool())
	toolReg.Register(agenttools.NewImageGenTool())
	toolReg.Register(agenttools.NewChromiumTool())
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

	// Register OpenRouter provider if API key is set
	openrouterKey := os.Getenv("OPENROUTER_API_KEY")
	if openrouterKey != "" {
		openrouterModel := os.Getenv("OPENROUTER_MODEL")
		if openrouterModel == "" {
			openrouterModel = "anthropic/claude-3.5-sonnet"
		}
		agentRuntime.RegisterProvider(agentproviders.NewOpenRouter(openrouterKey, openrouterModel, ""))
		fmt.Printf("registered OpenRouter provider (model: %s)\n", openrouterModel)
	}

	// Register Azure OpenAI provider if configured
	azureKey := os.Getenv("AZURE_OPENAI_API_KEY")
	if azureKey != "" {
		azureEndpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
		azureDeployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT")
		azureAPIVersion := os.Getenv("AZURE_OPENAI_API_VERSION")
		if azureEndpoint != "" && azureDeployment != "" {
			agentRuntime.RegisterProvider(agentproviders.NewAzureOpenAI(azureKey, azureEndpoint, azureDeployment, azureAPIVersion))
			fmt.Printf("registered Azure OpenAI provider (endpoint: %s, deployment: %s)\n", azureEndpoint, azureDeployment)
		}
	}

	log.Info("providers registered", map[string]any{"count": len(agentRuntime.Providers())})

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

	// Register Google Chat adapter if configured
	gchatWebhook := os.Getenv("GOOGLE_CHAT_WEBHOOK_URL")
	if gchatWebhook != "" {
		gchatTenant := os.Getenv("GOOGLE_CHAT_TENANT")
		if gchatTenant == "" {
			gchatTenant = "default"
		}
		gchatAgent := os.Getenv("GOOGLE_CHAT_AGENT")
		if gchatAgent == "" {
			gchatAgent = "assistant"
		}
		gchatAddr := os.Getenv("GOOGLE_CHAT_LISTEN_ADDR")
		if gchatAddr == "" {
			gchatAddr = "127.0.0.1:3006"
		}
		gchatAdapter := googlechatpkg.New(gchatAddr, gchatWebhook, gchatTenant, gchatAgent)
		channelMgr.AddAdapter(gchatAdapter)
		fmt.Printf("registered Google Chat adapter (tenant: %s, agent: %s, listen: %s)\n", gchatTenant, gchatAgent, gchatAddr)
	}

	proxyGateway := proxy.NewGateway(cfg.Listen.Address)
	proxyUpstream := os.Getenv("PROXY_UPSTREAM_URL")
	if proxyUpstream == "" {
		proxyUpstream = "https://api.anthropic.com"
	}
	proxyGateway.SetUpstreamURL(proxyUpstream)
	skillRuntime := skill.NewRuntime()
	skillRuntime.SetOpenClawLoader(compat.LoadOpenClawSkillFromFile)
	federationMod := federation.NewModule("cyntr-local")
	schedulerMod := scheduler.New()
	workflowEngine := workflow.New()

	k.Register(policyEngine)
	k.Register(auditLogger)
	k.Register(agentRuntime)
	k.Register(channelMgr)
	k.Register(proxyGateway)
	k.Register(skillRuntime)
	k.Register(federationMod)
	k.Register(schedulerMod)
	k.Register(workflowEngine)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := k.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "start error: %v\n", err)
		os.Exit(1)
	}

	log.Info("kernel started", map[string]any{"modules": len(k.Modules())})

	// Start API + Dashboard server
	apiServer := webapi.NewServer(k.Bus(), k)
	tenantMgr, _ := tenant.NewManager(cfg, nil)
	apiServer.SetTenantManager(tenantMgr)
	dashboard := web.NewDashboardHandler()

	// Wrap API with auth if API key is configured
	var apiHandler http.Handler = apiServer
	if apiKey := os.Getenv("CYNTR_API_KEY"); apiKey != "" {
		apiHandler = webapi.NewAuthMiddleware(webapi.AuthConfig{
			Enabled: true,
			APIKeys: map[string]string{apiKey: "default"},
		}).Wrap(apiServer)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", apiHandler)
	mux.Handle("/", dashboard)

	webAddr := cfg.Listen.WebUI
	if webAddr == "" {
		webAddr = ":7700"
	}

	go func() {
		if err := http.ListenAndServe(webAddr, mux); err != nil {
			fmt.Fprintf(os.Stderr, "web server error: %v\n", err)
		}
	}()

	showPostStartBanner("http://localhost"+webAddr, "http://"+cfg.Listen.Address+"/api/v1/")

	// Auto-register cloud-ops agent if config file exists
	if agentData, err := os.ReadFile("cloud-ops-agent.json"); err == nil {
		var agentCfg agent.AgentConfig
		if json.Unmarshal(agentData, &agentCfg) == nil && agentCfg.Name != "" {
			regCtx, regCancel := context.WithTimeout(ctx, 5*time.Second)
			_, regErr := k.Bus().Request(regCtx, ipc.Message{
				Source: "startup", Target: "agent_runtime", Topic: "agent.create",
				Payload: agentCfg,
			})
			regCancel()
			if regErr == nil {
				fmt.Printf("registered cloud-ops agent (tenant: %s, name: %s)\n", agentCfg.Tenant, agentCfg.Name)
			}
		}
	}

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
