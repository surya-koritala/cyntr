package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
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
	"github.com/cyntr-dev/cyntr/modules/eval"
	"github.com/cyntr-dev/cyntr/modules/federation"
	"github.com/cyntr-dev/cyntr/modules/notify"
	"github.com/cyntr-dev/cyntr/modules/sla"
	"github.com/cyntr-dev/cyntr/modules/crew"
	"github.com/cyntr-dev/cyntr/modules/mcp"
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

const version = "1.1.0"

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
	case "chat":
		runChat(os.Args[2:])
	case "docs":
		runDocs(os.Args[2:])
	case "backup":
		runBackup(os.Args[2:])
	case "restore":
		runRestore(os.Args[2:])
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

	memoryStore, err := agent.NewMemoryStore("memory.db")
	if err != nil {
		log.Warn("memory store disabled", map[string]any{"error": err.Error()})
	}
	agentRuntime.SetMemoryStore(memoryStore)

	usageStore, _ := agent.NewUsageStore("usage.db")
	agentRuntime.SetUsageStore(usageStore)

	// Start data retention scheduler (cleans up old sessions/memories/usage)
	agent.StartRetentionScheduler(sessionStore, memoryStore, usageStore, agent.RetentionPolicy{
		SessionTTL: 90 * 24 * time.Hour,  // 90 days
		MemoryTTL:  180 * 24 * time.Hour, // 180 days
		UsageTTL:   365 * 24 * time.Hour, // 1 year
	}, 24*time.Hour)
	log.Info("retention scheduler started", map[string]any{"session_ttl": "90d", "memory_ttl": "180d", "usage_ttl": "365d"})

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
	toolReg.Register(agenttools.NewOrchestrateTool(k.Bus()))
	toolReg.Register(agenttools.NewSkillRouterTool(k.Bus()))
	toolReg.Register(agenttools.NewCodeInterpreterTool())
	toolReg.Register(agenttools.NewTranscribeTool())
	toolReg.Register(agenttools.NewWebSearchTool())
	toolReg.Register(agenttools.NewPDFReaderTool())
	toolReg.Register(agenttools.NewDatabaseTool())
	toolReg.Register(agenttools.NewImageGenTool())
	toolReg.Register(agenttools.NewChromiumTool())
	toolReg.Register(agenttools.NewSendMessageTool(k.Bus()))
	toolReg.Register(agenttools.NewKubectlTool())
	toolReg.Register(agenttools.NewJSONQueryTool())
	toolReg.Register(agenttools.NewCSVQueryTool())
	toolReg.Register(agenttools.NewSendNotificationTool())

	// Load custom YAML tools from tools/ directory
	yamlTools, err := agenttools.LoadToolsFromDir("tools")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: YAML tools: %v\n", err)
	} else {
		for _, yt := range yamlTools {
			toolReg.Register(yt)
			log.Info("YAML tool registered", map[string]any{"name": yt.Name()})
		}
	}

	// Knowledge base tool
	knowledgeTool, err := agenttools.NewKnowledgeTool("knowledge_base.db")
	if err == nil {
		toolReg.Register(knowledgeTool)
		webapi.SetKnowledgeTool(knowledgeTool)
		toolReg.Register(agenttools.NewRunbookTool(knowledgeTool))
	} else {
		fmt.Fprintf(os.Stderr, "warning: knowledge base disabled: %v\n", err)
	}

	// AWS tools
	toolReg.Register(agenttools.NewAWSTool())
	toolReg.Register(agenttools.NewCostExplorerTool())
	toolReg.Register(agenttools.NewAlatirokTool())

	agentRuntime.SetToolRegistry(toolReg)

	// Register Claude providers if API key is set
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	if anthropicKey != "" {
		// Register multiple Anthropic models if ANTHROPIC_MODELS is set
		if models := os.Getenv("ANTHROPIC_MODELS"); models != "" {
			for _, m := range strings.Split(models, ",") {
				m = strings.TrimSpace(m)
				if m == "" {
					continue
				}
				p := agentproviders.NewAnthropic(anthropicKey, m, "")
				agentRuntime.RegisterProvider(p)
				log.Info("provider registered", map[string]any{"provider": p.Name(), "model": m})
			}
		} else {
			// Single model (backward compat)
			claudeModel := os.Getenv("ANTHROPIC_MODEL")
			if claudeModel == "" {
				claudeModel = "claude-sonnet-4-20250514"
			}
			p := agentproviders.NewAnthropic(anthropicKey, claudeModel, "")
			agentRuntime.RegisterProvider(p)
			log.Info("provider registered", map[string]any{"provider": p.Name(), "model": claudeModel})
		}
	}

	// Register OpenAI provider if API key is set
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey != "" {
		openaiModel := os.Getenv("OPENAI_MODEL")
		if openaiModel == "" {
			openaiModel = "gpt-4"
		}
		agentRuntime.RegisterProvider(agentproviders.NewOpenAI(openaiKey, openaiModel, ""))
		log.Info("provider registered", map[string]any{"provider": "gpt", "model": openaiModel})
	}

	// Register Gemini provider if API key is set
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey != "" {
		geminiModel := os.Getenv("GEMINI_MODEL")
		if geminiModel == "" {
			geminiModel = "gemini-pro"
		}
		agentRuntime.RegisterProvider(agentproviders.NewGemini(geminiKey, geminiModel, ""))
		log.Info("provider registered", map[string]any{"provider": "gemini", "model": geminiModel})
	}

	// Register Ollama provider if URL is set
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL != "" {
		ollamaModel := os.Getenv("OLLAMA_MODEL")
		if ollamaModel == "" {
			ollamaModel = "llama3"
		}
		agentRuntime.RegisterProvider(agentproviders.NewOllama(ollamaModel, ollamaURL))
		log.Info("provider registered", map[string]any{"provider": "ollama", "model": ollamaModel})
	}

	// Register OpenRouter provider if API key is set
	openrouterKey := os.Getenv("OPENROUTER_API_KEY")
	if openrouterKey != "" {
		openrouterModel := os.Getenv("OPENROUTER_MODEL")
		if openrouterModel == "" {
			openrouterModel = "anthropic/claude-3.5-sonnet"
		}
		agentRuntime.RegisterProvider(agentproviders.NewOpenRouter(openrouterKey, openrouterModel, ""))
		log.Info("provider registered", map[string]any{"provider": "openrouter", "model": openrouterModel})
	}

	// Register Azure OpenAI providers
	// Supports single deployment (AZURE_OPENAI_DEPLOYMENT) or multiple (AZURE_OPENAI_DEPLOYMENTS=gpt-4.1,gpt-5-chat,...)
	azureKey := os.Getenv("AZURE_OPENAI_API_KEY")
	if azureKey != "" {
		azureEndpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
		azureAPIVersion := os.Getenv("AZURE_OPENAI_API_VERSION")

		// Multiple deployments
		if deployments := os.Getenv("AZURE_OPENAI_DEPLOYMENTS"); deployments != "" && azureEndpoint != "" {
			for _, dep := range strings.Split(deployments, ",") {
				dep = strings.TrimSpace(dep)
				if dep == "" {
					continue
				}
				agentRuntime.RegisterProvider(agentproviders.NewAzureOpenAI(azureKey, azureEndpoint, dep, azureAPIVersion))
				log.Info("provider registered", map[string]any{"provider": dep, "endpoint": azureEndpoint})
			}
		} else if azureDeployment := os.Getenv("AZURE_OPENAI_DEPLOYMENT"); azureDeployment != "" && azureEndpoint != "" {
			// Single deployment (backward compat)
			agentRuntime.RegisterProvider(agentproviders.NewAzureOpenAI(azureKey, azureEndpoint, azureDeployment, azureAPIVersion))
			log.Info("provider registered", map[string]any{"provider": azureDeployment, "endpoint": azureEndpoint})
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
		if slackRoutes := os.Getenv("SLACK_ROUTES"); slackRoutes != "" {
			routes := make(map[string]string)
			for _, pair := range strings.Split(slackRoutes, ",") {
				parts := strings.SplitN(strings.TrimSpace(pair), "=", 2)
				if len(parts) == 2 {
					routes[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
			}
			slackAdapter.SetRoutes(routes)
		}
		if os.Getenv("SLACK_USE_THREADS") == "true" {
			slackAdapter.SetUseThreads(true)
		}
		channelMgr.AddAdapter(slackAdapter)
		log.Info("channel adapter registered", map[string]any{"adapter": "slack", "tenant": slackTenant, "agent": slackAgent, "listen": slackAddr})
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
		log.Info("channel adapter registered", map[string]any{"adapter": "teams", "tenant": teamsTenant, "agent": teamsAgent, "listen": teamsAddr})
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
		log.Info("channel adapter registered", map[string]any{"adapter": "email", "tenant": emailTenant, "agent": emailAgent, "listen": emailAddr})
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
		log.Info("channel adapter registered", map[string]any{"adapter": "whatsapp", "tenant": waTenant, "agent": waAgent, "listen": waAddr})
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
		log.Info("channel adapter registered", map[string]any{"adapter": "telegram", "tenant": tgTenant, "agent": tgAgent, "listen": tgAddr})
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
		log.Info("channel adapter registered", map[string]any{"adapter": "discord", "tenant": discordTenant, "agent": discordAgent, "listen": discordAddr})
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
		log.Info("channel adapter registered", map[string]any{"adapter": "google_chat", "tenant": gchatTenant, "agent": gchatAgent, "listen": gchatAddr})
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
	schedulerMod := scheduler.New("scheduler_jobs.json")
	workflowEngine := workflow.New()

	// MCP module
	mcpManager := mcp.NewManager(toolReg)
	// Parse MCP config from env or yaml
	if mcpJSON := os.Getenv("MCP_SERVERS"); mcpJSON != "" {
		var mcpConfigs []mcp.ServerConfig
		if json.Unmarshal([]byte(mcpJSON), &mcpConfigs) == nil {
			mcpManager.SetConfigs(mcpConfigs)
		}
	}

	k.Register(policyEngine)
	k.Register(auditLogger)
	k.Register(agentRuntime)
	k.Register(channelMgr)
	k.Register(proxyGateway)
	k.Register(skillRuntime)
	k.Register(federationMod)
	k.Register(schedulerMod)
	k.Register(workflowEngine)
	k.Register(mcpManager)

	// Crew engine
	crewEngine := crew.New()
	k.Register(crewEngine)

	// Eval runner
	evalRunner := eval.New()
	k.Register(evalRunner)

	// Notification channels
	notifierInst := notify.NewNotifier()
	if pdKey := os.Getenv("PAGERDUTY_ROUTING_KEY"); pdKey != "" {
		notifierInst.AddChannel(notify.NewPagerDutyChannel("", pdKey))
		log.Info("notification channel registered", map[string]any{"channel": "pagerduty"})
	}
	if ddKey := os.Getenv("DATADOG_API_KEY"); ddKey != "" {
		notifierInst.AddChannel(notify.NewDatadogChannel("", ddKey))
		log.Info("notification channel registered", map[string]any{"channel": "datadog"})
	}
	if webhookURL := os.Getenv("NOTIFY_WEBHOOK_URL"); webhookURL != "" {
		notifierInst.AddChannel(notify.NewGenericWebhookChannel("webhook", webhookURL, nil))
		log.Info("notification channel registered", map[string]any{"channel": "webhook"})
	}

	// SLA monitoring
	slaMonitor := sla.New()
	slaMonitor.SetNotifier(notifierInst)
	k.Register(slaMonitor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := k.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "start error: %v\n", err)
		os.Exit(1)
	}

	log.Info("kernel started", map[string]any{"modules": len(k.Modules())})

	// Real-time event streaming for dashboard
	eventBroker := web.NewEventBroker()
	k.Bus().Subscribe("events", "agent.activity", func(msg ipc.Message) (ipc.Message, error) {
		if evt, ok := msg.Payload.(agent.ActivityEvent); ok {
			eventBroker.Broadcast("activity", evt)
		}
		return ipc.Message{}, nil
	})

	// Approval notifications to Slack
	if approvalChannel := os.Getenv("SLACK_APPROVAL_CHANNEL"); approvalChannel != "" {
		k.Bus().Subscribe("notifications", "agent.activity", func(msg ipc.Message) (ipc.Message, error) {
			if evt, ok := msg.Payload.(agent.ActivityEvent); ok && evt.Type == "approval_needed" {
				k.Bus().Request(context.Background(), ipc.Message{
					Source: "notifications", Target: "channel", Topic: "channel.send",
					Payload: channel.OutboundMessage{
						Channel:   "slack",
						ChannelID: approvalChannel,
						Text:      fmt.Sprintf("*Approval Required*\nAgent: %s\nTenant: %s\n%s\n\nApprove via dashboard: /api/v1/approvals", evt.Agent, evt.Tenant, evt.Detail),
					},
				})
			}
			return ipc.Message{}, nil
		})
	}

	// Start API + Dashboard server
	apiServer := webapi.NewServer(k.Bus(), k)
	tenantMgr, err := tenant.NewManager(cfg, nil)
	if err != nil {
		log.Warn("tenant manager init failed", map[string]any{"error": err.Error()})
	}
	apiServer.SetTenantManager(tenantMgr)
	apiServer.SetNotifier(notifierInst)
	webapi.SetSessionStore(sessionStore)
	webapi.SetUsageStore(usageStore)
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
	mux.Handle("/events", eventBroker)
	mux.Handle("/", dashboard)

	// Wrap with metrics middleware to count requests/latency/errors
	metricsHandler := webapi.MetricsMiddleware(mux)

	webAddr := cfg.Listen.WebUI
	if webAddr == "" {
		webAddr = ":7700"
	}

	go func() {
		if err := http.ListenAndServe(webAddr, metricsHandler); err != nil {
			log.Error("web server error", map[string]any{"addr": webAddr, "error": err.Error()})
		}
	}()

	showPostStartBanner("http://localhost"+webAddr, "http://"+cfg.Listen.Address+"/api/v1/")

	// Auto-register agents from *-agent.json files
	entries, _ := os.ReadDir(".")
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "-agent.json") {
			continue
		}
		agentData, err := os.ReadFile(entry.Name())
		if err != nil {
			continue
		}
		var agentCfg agent.AgentConfig
		if json.Unmarshal(agentData, &agentCfg) == nil && agentCfg.Name != "" {
			regCtx, regCancel := context.WithTimeout(ctx, 5*time.Second)
			_, regErr := k.Bus().Request(regCtx, ipc.Message{
				Source: "startup", Target: "agent_runtime", Topic: "agent.create",
				Payload: agentCfg,
			})
			regCancel()
			if regErr == nil {
				log.Info("agent registered from file", map[string]any{"name": agentCfg.Name, "file": entry.Name()})
			}
		}
	}

	// Auto-import local OpenClaw skills
	openclawDirs := []string{
		"/private/tmp/openclaw-skills",
		os.Getenv("HOME") + "/.openclaw/skills",
	}
	for _, dir := range openclawDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillPath := dir + "/" + entry.Name() + "/SKILL.md"
			if _, err := os.Stat(skillPath); err != nil {
				continue
			}
			importCtx, importCancel := context.WithTimeout(ctx, 5*time.Second)
			resp, importErr := k.Bus().Request(importCtx, ipc.Message{
				Source: "startup", Target: "skill_runtime", Topic: "skill.import_openclaw",
				Payload: skillPath,
			})
			importCancel()
			if importErr == nil {
				if name, ok := resp.Payload.(string); ok {
					log.Info("OpenClaw skill imported", map[string]any{"name": name, "path": skillPath})
				}
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
				k.Bus().Publish(ipc.Message{Source: "kernel", Topic: "config.reloaded"})
				log.Info("config reloaded via SIGHUP", nil)
				fmt.Println("config reloaded")
			}
		case syscall.SIGINT, syscall.SIGTERM:
			log.Info("shutdown initiated", map[string]any{"signal": sig.String()})

			// Create shutdown context with 30s deadline
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer shutdownCancel()

			// Stop kernel (stops all modules in reverse order)
			if err := k.Stop(shutdownCtx); err != nil {
				log.Error("shutdown error", map[string]any{"error": err.Error()})
				os.Exit(1)
			}
			log.Info("cyntr stopped gracefully", nil)
			return
		}
	}
}
