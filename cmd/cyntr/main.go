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
	slackpkg "github.com/cyntr-dev/cyntr/modules/channel/slack"
	"github.com/cyntr-dev/cyntr/modules/federation"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/modules/proxy"
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

	agentRuntime.RegisterProvider(agentproviders.NewMock("Default mock response"))

	// Register tools
	toolReg := agent.NewToolRegistry()
	toolReg.Register(&agenttools.ShellTool{})
	toolReg.Register(agenttools.NewHTTPTool())
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

	proxyGateway := proxy.NewGateway(cfg.Listen.Address)
	skillRuntime := skill.NewRuntime()
	skillRuntime.SetOpenClawLoader(compat.LoadOpenClawSkillFromFile)
	federationMod := federation.NewModule("cyntr-local")

	k.Register(policyEngine)
	k.Register(auditLogger)
	k.Register(agentRuntime)
	k.Register(channelMgr)
	k.Register(proxyGateway)
	k.Register(skillRuntime)
	k.Register(federationMod)

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
