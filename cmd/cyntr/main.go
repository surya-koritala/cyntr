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
	"github.com/cyntr-dev/cyntr/modules/audit"
	"github.com/cyntr-dev/cyntr/modules/channel"
	"github.com/cyntr-dev/cyntr/modules/federation"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/modules/proxy"
	"github.com/cyntr-dev/cyntr/modules/skill"
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
		fmt.Println("cyntr status: use the API at /api/v1/system/health")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "usage: cyntr <command>")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  start [config]  Start the Cyntr server")
	fmt.Fprintln(os.Stderr, "  status          Show server status")
	fmt.Fprintln(os.Stderr, "  version         Show version")
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

	channelMgr := channel.NewManager()
	proxyGateway := proxy.NewGateway(cfg.Listen.Address)
	skillRuntime := skill.NewRuntime()
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
