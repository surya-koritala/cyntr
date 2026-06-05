package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/config"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/jobs"
	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/agent"
	agentproviders "github.com/cyntr-dev/cyntr/modules/agent/providers"
	agenttools "github.com/cyntr-dev/cyntr/modules/agent/tools"
	"github.com/cyntr-dev/cyntr/modules/audit"
	"github.com/cyntr-dev/cyntr/modules/channel"
	discordpkg "github.com/cyntr-dev/cyntr/modules/channel/discord"
	emailpkg "github.com/cyntr-dev/cyntr/modules/channel/email"
	googlechatpkg "github.com/cyntr-dev/cyntr/modules/channel/googlechat"
	matrixpkg "github.com/cyntr-dev/cyntr/modules/channel/matrix"
	mattermostpkg "github.com/cyntr-dev/cyntr/modules/channel/mattermost"
	signalpkg "github.com/cyntr-dev/cyntr/modules/channel/signal"
	slackpkg "github.com/cyntr-dev/cyntr/modules/channel/slack"
	smspkg "github.com/cyntr-dev/cyntr/modules/channel/sms"
	teamspkg "github.com/cyntr-dev/cyntr/modules/channel/teams"
	telegrampkg "github.com/cyntr-dev/cyntr/modules/channel/telegram"
	whatsapppkg "github.com/cyntr-dev/cyntr/modules/channel/whatsapp"
	"github.com/cyntr-dev/cyntr/modules/crew"
	"github.com/cyntr-dev/cyntr/modules/curator"
	"github.com/cyntr-dev/cyntr/modules/eval"
	"github.com/cyntr-dev/cyntr/modules/federation"
	"github.com/cyntr-dev/cyntr/modules/learn"
	"github.com/cyntr-dev/cyntr/modules/mcp"
	"github.com/cyntr-dev/cyntr/modules/notify"
	"github.com/cyntr-dev/cyntr/modules/observability"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/modules/proxy"
	"github.com/cyntr-dev/cyntr/modules/quota"
	"github.com/cyntr-dev/cyntr/modules/recall"
	"github.com/cyntr-dev/cyntr/modules/scheduler"
	"github.com/cyntr-dev/cyntr/modules/skill"
	"github.com/cyntr-dev/cyntr/modules/skill/compat"
	"github.com/cyntr-dev/cyntr/modules/sla"
	"github.com/cyntr-dev/cyntr/modules/usermodel"
	"github.com/cyntr-dev/cyntr/modules/workflow"
	"github.com/cyntr-dev/cyntr/packs/loomfeed"
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
	case "eval":
		runEval(os.Args[2:])
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

	// Optional Rego policy: enabled if policy.rego (file) or policy.rego.d (dir) exists.
	regoPath := ""
	for _, candidate := range []string{"policy.rego", "policy.rego.d"} {
		if _, err := os.Stat(candidate); err == nil {
			regoPath = candidate
			break
		}
	}

	// Register all modules
	policyEngine := policy.NewEngine(policyPath, regoPath)
	auditLogger := audit.NewLogger(dataPath("audit.db"), "cyntr-local", "audit-secret")
	agentRuntime := agent.NewRuntime()

	sessionStore, err := agent.NewSessionStore(dataPath("sessions.db"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "session store error: %v\n", err)
		os.Exit(1)
	}
	agentRuntime.SetSessionStore(sessionStore)

	memoryStore, err := agent.NewMemoryStore(dataPath("memory.db"))
	if err != nil {
		log.Warn("memory store disabled", map[string]any{"error": err.Error()})
	}
	agentRuntime.SetMemoryStore(memoryStore)

	usageStore, _ := agent.NewUsageStore(dataPath("usage.db"))
	agentRuntime.SetUsageStore(usageStore)

	// Per-workspace context files (A7): AGENTS.md / CYNTR.md / SOUL.md /
	// TOOLS.md under <workspace>/<tenant>/<agent>/ are prepended to every
	// chat's system context. Root configurable via CYNTR_WORKSPACE_DIR.
	workspaceRoot := os.Getenv("CYNTR_WORKSPACE_DIR")
	if workspaceRoot == "" {
		workspaceRoot = "workspace"
	}
	agentRuntime.SetContextLoader(agent.NewContextLoader(workspaceRoot))

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

	// Build shell tool. If shell_exec_policies is configured (and at least
	// one entry asks for the docker backend), wire in a per-tenant selector
	// so those tenants get container-isolated execution. Otherwise the tool
	// keeps its legacy in-process behavior — opt-in, no breaking change.
	shellTool := &agenttools.ShellTool{}
	if policies := buildShellPolicies(cfg.ShellExecPolicies); len(policies) > 0 {
		selector, err := agenttools.NewDockerBackendSelector(policies)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: shell_exec_policies: %v\n", err)
		} else {
			shellTool.BackendSelector = selector
			log.Info("shell exec backend selector wired", map[string]any{"policies": len(policies)})
		}
	}
	toolReg.Register(shellTool)
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
	codeTool := agenttools.NewCodeInterpreterTool()
	// Tool-RPC bridge (E21): scripts may call cyntr.call_tool(...) in one turn.
	// Every call re-checks policy and is audited, so scripting can't reach a
	// tool the agent itself couldn't call.
	codeAudit := agent.NewBusAuditEmitter(k.Bus(), os.Getenv("CYNTR_NODE_ID"))
	codeTool.EnableRPC(&agenttools.RPCConfig{
		Exec: func(c context.Context, name string, args map[string]string) (string, error) {
			return toolReg.Execute(c, name, args)
		},
		PolicyCheck: func(tenant, user, ag, tool string) string {
			pctx, pcancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer pcancel()
			resp, perr := k.Bus().Request(pctx, ipc.Message{
				Source: "code_rpc", Target: "policy", Topic: "policy.check",
				Payload: policy.CheckRequest{Tenant: tenant, Action: "tool_call", Tool: tool, Agent: ag, User: user},
			})
			if perr != nil {
				if perr == ipc.ErrNoHandler {
					return "allow"
				}
				return "deny"
			}
			cr, ok := resp.Payload.(policy.CheckResponse)
			if !ok {
				return "deny"
			}
			return cr.Decision.String()
		},
		Audit: func(tenant, user, ag, tool, decision string) {
			codeAudit.Emit("tool_call.script", tenant, user, decision, map[string]string{"tool": tool, "agent": ag})
		},
	})
	toolReg.Register(codeTool)
	toolReg.Register(agenttools.NewTranscribeTool())
	toolReg.Register(agenttools.NewWebSearchTool())
	toolReg.Register(agenttools.NewWebReaderTool())
	toolReg.Register(agenttools.NewPDFReaderTool())
	toolReg.Register(agenttools.NewDatabaseTool())
	toolReg.Register(agenttools.NewImageGenTool())
	toolReg.Register(agenttools.NewChromiumTool())
	toolReg.Register(agenttools.NewSendMessageTool(k.Bus()))
	toolReg.Register(agenttools.NewKubectlTool())
	toolReg.Register(agenttools.NewJSONQueryTool())
	toolReg.Register(agenttools.NewCSVQueryTool())
	toolReg.Register(agenttools.NewSendNotificationTool())
	toolReg.Register(agenttools.NewToolPlanTool(toolReg, k.Bus()))
	toolReg.Register(agenttools.NewUserModelReadTool(k.Bus()))
	toolReg.Register(agenttools.NewUserModelWriteTool(k.Bus()))
	toolReg.Register(agenttools.NewRecallSearchTool(k.Bus()))

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
	knowledgeTool, err := agenttools.NewKnowledgeTool(dataPath("knowledge_base.db"))
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

	// Optional packs — opt-in only. Default: no vertical packs registered, so
	// the binary ships as a pure enterprise agent platform.
	if packEnabled(cfg.Packs, "loomfeed", "CYNTR_PACK_LOOMFEED") {
		alatirokTool := loomfeed.NewAlatirokTool()
		newsTool := loomfeed.NewNewsAggregatorTool()
		toolReg.Register(alatirokTool)
		toolReg.Register(newsTool)
		toolReg.Register(loomfeed.NewAlatirokPipelineTool(newsTool, alatirokTool))
		log.Info("pack registered", map[string]any{"pack": "loomfeed", "tools": []string{"alatirok", "news_aggregator", "alatirok_pipeline"}})
	}

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

	// Register Ollama providers — supports multiple models via OLLAMA_MODELS=gemma4,qwen3:8b,...
	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = os.Getenv("OLLAMA_HOST")
	}
	if ollamaURL != "" {
		if models := os.Getenv("OLLAMA_MODELS"); models != "" {
			for _, model := range strings.Split(models, ",") {
				model = strings.TrimSpace(model)
				if model == "" {
					continue
				}
				agentRuntime.RegisterProvider(agentproviders.NewOllama(model, ollamaURL))
				log.Info("provider registered", map[string]any{"provider": "ollama", "model": model})
			}
		} else {
			ollamaModel := os.Getenv("OLLAMA_MODEL")
			if ollamaModel == "" {
				ollamaModel = "llama3"
			}
			agentRuntime.RegisterProvider(agentproviders.NewOllama(ollamaModel, ollamaURL))
			log.Info("provider registered", map[string]any{"provider": "ollama", "model": ollamaModel})
		}
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

	// Generic OpenAI-compatible providers (D17): register any endpoint without
	// per-vendor code. CYNTR_COMPAT_PROVIDERS is a comma-separated list of
	// "name|baseURL|model|API_KEY_ENV" entries — e.g. NovitaAI, z.ai/GLM,
	// Kimi/Moonshot, MiniMax, NVIDIA NIM, vLLM, LM Studio.
	for _, spec := range strings.Split(os.Getenv("CYNTR_COMPAT_PROVIDERS"), ",") {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		parts := strings.Split(spec, "|")
		if len(parts) != 4 {
			log.Warn("invalid CYNTR_COMPAT_PROVIDERS entry (want name|baseURL|model|API_KEY_ENV)", map[string]any{"entry": spec})
			continue
		}
		name, baseURL, model, keyEnv := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]), strings.TrimSpace(parts[3])
		key := os.Getenv(keyEnv)
		if key == "" {
			log.Warn("OpenAI-compatible provider skipped: API key env not set", map[string]any{"provider": name, "key_env": keyEnv})
			continue
		}
		agentRuntime.RegisterProvider(agentproviders.NewOpenAICompatible(name, key, model, baseURL))
		log.Info("provider registered", map[string]any{"provider": name, "model": model, "compatible": true})
	}

	// Model failover (D18): CYNTR_MODEL_FALLBACKS="p1,p2,p3" registers a
	// "failover" provider that tries each already-registered provider in order,
	// advancing on transient errors (rate-limit/5xx/timeout) and rotating keys
	// on auth failures. Agents target it via model: failover.
	if fb := strings.TrimSpace(os.Getenv("CYNTR_MODEL_FALLBACKS")); fb != "" {
		var chain []agent.ModelProvider
		var names []string
		for _, n := range strings.Split(fb, ",") {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			if p := agentRuntime.Provider(n); p != nil {
				chain = append(chain, p)
				names = append(names, n)
			} else {
				log.Warn("failover: unknown provider, skipped", map[string]any{"provider": n})
			}
		}
		if len(chain) > 0 {
			fp := agent.NewFailoverProvider("failover", chain...)
			fp.SetOnAttempt(func(provider string, err error) {
				if err != nil {
					log.Warn("model failover attempt failed", map[string]any{"provider": provider, "error": err.Error()})
				}
			})
			agentRuntime.RegisterProvider(fp)
			log.Info("model failover registered", map[string]any{"chain": names})
		}
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

	// DM pairing (B12): gate untrusted inbound. CYNTR_DM_POLICY sets the
	// default policy (open|pairing|closed; default open for back-compat) and
	// CYNTR_DM_POLICIES="slack=pairing,web=open" overrides per channel.
	if pairingStore, perr := channel.NewPairingStore(dataPath("pairing.db")); perr != nil {
		log.Warn("DM pairing disabled", map[string]any{"error": perr.Error()})
	} else {
		gate := channel.NewGate(pairingStore, os.Getenv("CYNTR_DM_POLICY"))
		for _, entry := range strings.Split(os.Getenv("CYNTR_DM_POLICIES"), ",") {
			if kv := strings.SplitN(strings.TrimSpace(entry), "=", 2); len(kv) == 2 && kv[0] != "" {
				gate.SetPolicy(strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1]), nil)
			}
		}
		channelMgr.SetGate(gate)
		log.Info("DM pairing gate installed", map[string]any{"default": os.Getenv("CYNTR_DM_POLICY")})
	}

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

	// Register Mattermost adapter if configured
	mmWebhook := os.Getenv("MATTERMOST_WEBHOOK_URL")
	if mmWebhook != "" {
		mmTenant := os.Getenv("MATTERMOST_TENANT")
		if mmTenant == "" {
			mmTenant = "default"
		}
		mmAgent := os.Getenv("MATTERMOST_AGENT")
		if mmAgent == "" {
			mmAgent = "assistant"
		}
		mmAddr := os.Getenv("MATTERMOST_LISTEN_ADDR")
		if mmAddr == "" {
			mmAddr = "127.0.0.1:3007"
		}
		mmAdapter := mattermostpkg.New(mmAddr, mmWebhook, mmTenant, mmAgent)
		channelMgr.AddAdapter(mmAdapter)
		log.Info("channel adapter registered", map[string]any{"adapter": "mattermost", "tenant": mmTenant, "agent": mmAgent, "listen": mmAddr})
	}

	// Register Signal adapter if configured
	signalURL := os.Getenv("SIGNAL_CLI_URL")
	if signalURL != "" {
		signalTenant := os.Getenv("SIGNAL_TENANT")
		if signalTenant == "" {
			signalTenant = "default"
		}
		signalAgent := os.Getenv("SIGNAL_AGENT")
		if signalAgent == "" {
			signalAgent = "assistant"
		}
		signalAddr := os.Getenv("SIGNAL_LISTEN_ADDR")
		if signalAddr == "" {
			signalAddr = "127.0.0.1:3008"
		}
		signalAdapter := signalpkg.New(signalAddr, signalURL, signalTenant, signalAgent)
		channelMgr.AddAdapter(signalAdapter)
		log.Info("channel adapter registered", map[string]any{"adapter": "signal", "tenant": signalTenant, "agent": signalAgent, "listen": signalAddr})
	}

	// Register Matrix adapter if configured
	matrixHomeserver := os.Getenv("MATRIX_HOMESERVER_URL")
	matrixToken := os.Getenv("MATRIX_ACCESS_TOKEN")
	if matrixHomeserver != "" && matrixToken != "" {
		matrixTenant := os.Getenv("MATRIX_TENANT")
		if matrixTenant == "" {
			matrixTenant = "default"
		}
		matrixAgent := os.Getenv("MATRIX_AGENT")
		if matrixAgent == "" {
			matrixAgent = "assistant"
		}
		matrixAddr := os.Getenv("MATRIX_LISTEN_ADDR")
		if matrixAddr == "" {
			matrixAddr = "127.0.0.1:3009"
		}
		matrixAdapter := matrixpkg.New(matrixAddr, matrixHomeserver, matrixToken, matrixTenant, matrixAgent)
		channelMgr.AddAdapter(matrixAdapter)
		log.Info("channel adapter registered", map[string]any{"adapter": "matrix", "tenant": matrixTenant, "agent": matrixAgent, "listen": matrixAddr})
	}

	// Register SMS (Twilio) adapter if configured
	twilioSID := os.Getenv("TWILIO_ACCOUNT_SID")
	twilioToken := os.Getenv("TWILIO_AUTH_TOKEN")
	twilioFrom := os.Getenv("TWILIO_FROM")
	if twilioSID != "" && twilioToken != "" && twilioFrom != "" {
		smsTenant := os.Getenv("SMS_TENANT")
		if smsTenant == "" {
			smsTenant = "default"
		}
		smsAgent := os.Getenv("SMS_AGENT")
		if smsAgent == "" {
			smsAgent = "assistant"
		}
		smsAddr := os.Getenv("SMS_LISTEN_ADDR")
		if smsAddr == "" {
			smsAddr = "127.0.0.1:3010"
		}
		smsAdapter := smspkg.New(smsAddr, twilioSID, twilioToken, twilioFrom, smsTenant, smsAgent)
		channelMgr.AddAdapter(smsAdapter)
		log.Info("channel adapter registered", map[string]any{"adapter": "sms", "tenant": smsTenant, "agent": smsAgent, "listen": smsAddr})
	}

	proxyGateway := proxy.NewGateway(cfg.Listen.Address)
	proxyUpstream := os.Getenv("PROXY_UPSTREAM_URL")
	if proxyUpstream == "" {
		proxyUpstream = "https://api.anthropic.com"
	}
	proxyGateway.SetUpstreamURL(proxyUpstream)
	skillRuntime := skill.NewRuntime()
	skillRuntime.SetOpenClawLoader(compat.LoadOpenClawSkillFromFile)
	// Autonomous skill creation (A2): persist proposed skills as pending
	// candidates for approval. CYNTR_SKILL_AUTOACTIVATE_SAFE=true lets the
	// platform auto-activate proposals with safe capabilities (no shell/
	// network/filesystem); anything riskier always waits for an operator.
	if skillCandidates, scErr := skill.NewCandidateStore(dataPath("skill_candidates.db")); scErr != nil {
		log.Warn("skill candidate store disabled", map[string]any{"error": scErr.Error()})
	} else {
		skillRuntime.SetCandidateStore(skillCandidates)
		skillRuntime.SetAutoActivateSafe(os.Getenv("CYNTR_SKILL_AUTOACTIVATE_SAFE") == "true")
	}
	nodeID := os.Getenv("CYNTR_NODE_ID")
	if nodeID == "" {
		nodeID = "cyntr-local"
	}
	federationMod := federation.NewModule(nodeID)
	schedulerMod := scheduler.New(dataPath("scheduler_jobs.json"))
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

	// Observability is registered first so its Init runs before any other
	// module's, configuring the global OTel providers in time for those
	// modules to grab tracers/meters via the global accessors.
	obsModule := observability.New()
	k.Register(obsModule)

	k.Register(policyEngine)
	k.Register(auditLogger)
	k.Register(agentRuntime)
	k.Register(channelMgr)
	k.Register(proxyGateway)
	k.Register(quota.New(dataPath("quota.db")))
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

	// Curator (F3 — records skill invocations, exposes scores + prune suggestions)
	curatorMod := curator.New(dataPath("curator.db"))
	k.Register(curatorMod)

	// Self-improving skills (A3): let the curator turn a failing skill's recent
	// failures into an approval-gated improved candidate. The proposal flows
	// through skill.propose, so it is never auto-applied with risky caps; on
	// approval the live skill is replaced and the prior version kept for rollback.
	var improveProvider agent.ModelProvider
	for _, name := range agentRuntime.Providers() {
		if name == "mock" {
			continue // skip the always-registered mock provider in best-effort fallbacks
		}
		if p := agentRuntime.Provider(name); p != nil {
			improveProvider = p
			break
		}
	}
	if improveProvider != nil {
		cbus := k.Bus()
		fetchInstr := func(name string) (string, error) {
			rctx, rcancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer rcancel()
			resp, err := cbus.Request(rctx, ipc.Message{Source: "curator", Target: "skill_runtime", Topic: "skill.get", Payload: name})
			if err != nil {
				return "", err
			}
			s, ok := resp.Payload.(*skill.InstalledSkill)
			if !ok {
				return "", fmt.Errorf("skill.get: unexpected payload %T", resp.Payload)
			}
			return s.Instructions, nil
		}
		proposeImproved := func(name, description, instructions string) error {
			rctx, rcancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer rcancel()
			_, err := cbus.Request(rctx, ipc.Message{
				Source: "curator", Target: "skill_runtime", Topic: skill.TopicPropose,
				Payload: skill.ProposeRequest{Name: name, Description: description, Instructions: instructions, SourceAgent: "curator"},
			})
			return err
		}
		curatorMod.SetImprover(curator.NewImprover(improveProvider, "", fetchInstr, proposeImproved))
	}

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

	// User-model module — per-(tenant,user) curated profile + preferences
	// loaded into every chat's system context and editable via tools/API.
	userModelStore, err := usermodel.NewStore(dataPath("usermodel.db"))
	var userModelModule *usermodel.Module
	if err != nil {
		log.Warn("usermodel store disabled", map[string]any{"error": err.Error()})
	} else {
		userModelModule = usermodel.New(userModelStore)
		k.Register(userModelModule)
		// Per-tenant distillation (narrative profile + dialectic facts) is
		// opt-in. CYNTR_USERMODEL_DISTILL_TENANTS="default,research" turns it on
		// — without this there was no way to enable it outside tests.
		for _, t := range strings.Split(os.Getenv("CYNTR_USERMODEL_DISTILL_TENANTS"), ",") {
			if t = strings.TrimSpace(t); t != "" {
				if err := userModelStore.SetTenantDistillEnabled(t, true); err != nil {
					log.Warn("usermodel: enable distill failed", map[string]any{"tenant": t, "error": err.Error()})
				} else {
					log.Info("usermodel distillation enabled", map[string]any{"tenant": t})
				}
			}
		}
	}

	// Distiller: best-effort narrative profile updates from session history.
	// Opt-in per tenant (off by default). Runs on a cron schedule (default
	// 04:00 UTC daily, overridable via CYNTR_USERMODEL_DISTILL_CRON) and
	// can be invoked manually via POST /profile/distill.
	var distillTicker *usermodel.Ticker
	if userModelModule != nil {
		distillModelName := os.Getenv("CYNTR_USERMODEL_DISTILL_MODEL")
		if distillModelName == "" {
			distillModelName = usermodel.DefaultDistillModel
		}
		// Pick the cheapest registered provider matching the configured
		// name; fall back to any registered provider so distillation works
		// even when haiku isn't wired up. Skipped entirely when no
		// provider is available.
		var chosen agent.ModelProvider
		for _, name := range agentRuntime.Providers() {
			if name == distillModelName {
				chosen = agentRuntime.Provider(name)
				break
			}
		}
		if chosen == nil {
			// Best-effort fallback so dev environments without haiku still
			// see the path exercised. In production with a haiku key set,
			// this branch is never taken.
			for _, name := range agentRuntime.Providers() {
				if name == "mock" {
					continue // skip the always-registered mock provider in best-effort fallbacks
				}
				if p := agentRuntime.Provider(name); p != nil {
					chosen = p
					break
				}
			}
		}
		if chosen == nil {
			log.Warn("usermodel distiller disabled", map[string]any{"reason": "no LLM provider registered"})
		} else {
			adapter := agent.NewDistillProviderAdapter(chosen)
			emitter := agent.NewBusAuditEmitter(k.Bus(), nodeID)
			d, derr := usermodel.NewDistiller(usermodel.DistillerOptions{
				Store:       userModelStore,
				Provider:    adapter,
				Model:       distillModelName,
				Audit:       emitter,
				Concurrency: 5,
				EnableFacts: true,
				Logger: func(msg string, kv map[string]any) {
					log.Info(msg, kv)
				},
			})
			if derr != nil {
				log.Warn("usermodel distiller init failed", map[string]any{"error": derr.Error()})
			} else {
				userModelModule.SetDistiller(d)
				cronExpr := os.Getenv("CYNTR_USERMODEL_DISTILL_CRON")
				ticker, tickerErr := usermodel.NewTicker(d, cronExpr, func(msg string, kv map[string]any) {
					log.Info(msg, kv)
				})
				if tickerErr != nil {
					log.Warn("usermodel distill cron invalid", map[string]any{"error": tickerErr.Error()})
				} else {
					distillTicker = ticker
					log.Info("usermodel distiller registered", map[string]any{"model": distillModelName, "cron": cronExpr})
				}
			}
		}
	}
	_ = distillTicker // started after kernel boot below

	// Cross-session recall (A5) — full-text index over completed turns plus
	// per-session LLM summaries. The summary work runs on the shared
	// background job queue (F0.2); this is its first consumer.
	var recallJobQueue *jobs.Queue
	if recallStore, recallErr := recall.NewStore(dataPath("recall.db")); recallErr != nil {
		log.Warn("recall disabled", map[string]any{"error": recallErr.Error()})
	} else {
		// Pick a provider for summaries: the configured model if present,
		// otherwise any registered provider; nil disables summarization but
		// leaves search working.
		var sumProvider agent.ModelProvider
		if sumModel := os.Getenv("CYNTR_RECALL_SUMMARY_MODEL"); sumModel != "" {
			sumProvider = agentRuntime.Provider(sumModel)
		}
		if sumProvider == nil {
			for _, name := range agentRuntime.Providers() {
				if name == "mock" {
					continue // skip the always-registered mock provider in best-effort fallbacks
				}
				if p := agentRuntime.Provider(name); p != nil {
					sumProvider = p
					break
				}
			}
		}
		var summarizeFn recall.SummarizeFunc
		if sumProvider != nil {
			summarizeFn = func(c context.Context, conversation string) (string, error) {
				resp, err := sumProvider.Chat(c, []agent.Message{
					{Role: agent.RoleSystem, Content: "Summarize the following conversation in 2-4 sentences, focusing on decisions, facts, and open questions. Be concise."},
					{Role: agent.RoleUser, Content: conversation},
				}, nil)
				if err != nil {
					return "", err
				}
				return resp.Content, nil
			}
		}
		if q, qErr := jobs.NewQueue(dataPath("jobs.db"), jobs.WithLogger(func(m string, kv map[string]any) { log.Info(m, kv) })); qErr != nil {
			log.Warn("job queue disabled; recall summaries off", map[string]any{"error": qErr.Error()})
		} else {
			recallJobQueue = q
		}
		recallOpts := []recall.Option{recall.WithLogger(func(m string, kv map[string]any) { log.Info(m, kv) })}
		if recallJobQueue != nil && summarizeFn != nil {
			recallOpts = append(recallOpts,
				recall.WithQueue(recallJobQueue),
				recall.WithSummarizer(recall.NewSummarizer(recallStore, summarizeFn, 50)),
			)
		}
		k.Register(recall.New(recallStore, recallOpts...))
		log.Info("recall module registered", map[string]any{"summaries": recallJobQueue != nil && summarizeFn != nil})
	}

	// Closed learning loop (A1) — reflect on complex turns and persist what
	// was learned (a memory + an approval-gated skill proposal). Off unless
	// CYNTR_LEARN_ENABLED=true; reuses the shared job queue and a provider.
	learnEnabled := os.Getenv("CYNTR_LEARN_ENABLED") == "true"
	var learnReflect learn.ReflectFunc
	var learnProvider agent.ModelProvider
	for _, name := range agentRuntime.Providers() {
		if name == "mock" {
			continue // skip the always-registered mock provider in best-effort fallbacks
		}
		if p := agentRuntime.Provider(name); p != nil {
			learnProvider = p
			break
		}
	}
	if learnProvider != nil {
		learnReflect = func(c context.Context, prompt string) (string, error) {
			resp, err := learnProvider.Chat(c, []agent.Message{{Role: agent.RoleUser, Content: prompt}}, nil)
			if err != nil {
				return "", err
			}
			return resp.Content, nil
		}
	}
	learnOpts := []learn.Option{
		learn.WithReflectFunc(learnReflect),
		learn.WithLogger(func(m string, kv map[string]any) { log.Info(m, kv) }),
	}
	if recallJobQueue != nil {
		learnOpts = append(learnOpts, learn.WithQueue(recallJobQueue))
	}
	if n := os.Getenv("CYNTR_LEARN_MIN_TOOLCALLS"); n != "" {
		if v, err := strconv.Atoi(n); err == nil {
			learnOpts = append(learnOpts, learn.WithMinToolCalls(v))
		}
	}
	k.Register(learn.New(learnEnabled, learnOpts...))
	log.Info("learn module registered", map[string]any{"enabled": learnEnabled})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := k.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "start error: %v\n", err)
		os.Exit(1)
	}

	log.Info("kernel started", map[string]any{"modules": len(k.Modules())})

	// Kick off the usermodel distill cron loop now that the kernel is up.
	if distillTicker != nil {
		distillTicker.Start()
	}

	// Start the background job queue (drives recall summaries).
	if recallJobQueue != nil {
		recallJobQueue.Start()
	}

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
	// When observability is enabled the module exposes a Prometheus exposition
	// handler; mount it on the API server. When disabled this is a no-op (the
	// handler is nil and the endpoint returns 404).
	if promHandler := obsModule.PrometheusHandler(); promHandler != nil {
		apiServer.SetPrometheusHandler(promHandler)
	}
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

			// Drain the background job queue before stopping modules.
			if recallJobQueue != nil {
				recallJobQueue.Stop()
			}

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

// buildShellPolicies converts the YAML config form to the tools-package form.
// Returns nil when no policies are declared so callers can cheaply skip the
// selector wiring entirely.
func buildShellPolicies(in []config.ShellExecPolicyConfig) []agenttools.ShellExecPolicy {
	if len(in) == 0 {
		return nil
	}
	out := make([]agenttools.ShellExecPolicy, 0, len(in))
	for _, p := range in {
		out = append(out, agenttools.ShellExecPolicy{
			Tenant:  p.Tenant,
			Backend: p.Backend,
			Image:   p.Image,
			Timeout: p.Timeout,
		})
	}
	return out
}

// dataPath resolves a relative store filename against the CYNTR_DATA_DIR
// env var. When unset (the legacy default) it returns the name unchanged so
// SQLite stores land in the current working directory — exactly the
// behaviour pre-T2.5. When set (typical for hosted/container deployments)
// the directory is created on first use, and all store files live there so
// a single mounted volume covers durable state.
func dataPath(name string) string {
	dir := os.Getenv("CYNTR_DATA_DIR")
	if dir == "" {
		return name
	}
	// Best-effort: if MkdirAll fails the store opens will surface a clearer
	// error than a silent fallback would. Ignore the error here.
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, name)
}

// packEnabled returns true when the named pack is opted in via cyntr.yaml
// (packs.<name>: true) or via the matching environment variable set to "1".
func packEnabled(packs map[string]bool, name, envVar string) bool {
	if packs[name] {
		return true
	}
	if v := os.Getenv(envVar); v == "1" || strings.EqualFold(v, "true") {
		return true
	}
	return false
}
