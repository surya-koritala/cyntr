package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/agent/providers"
	"github.com/cyntr-dev/cyntr/modules/channel"
	"github.com/cyntr-dev/cyntr/modules/channel/webhook"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

func TestChannelWebhookEndToEnd(t *testing.T) {
	dir := t.TempDir()

	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte(`
rules:
  - name: allow-all
    tenant: "*"
    action: "*"
    tool: "*"
    agent: "*"
    decision: allow
    priority: 1
`), 0644)

	cfgPath := filepath.Join(dir, "cyntr.yaml")
	os.WriteFile(cfgPath, []byte("version: \"1\"\nlisten:\n  address: \"127.0.0.1:8080\"\n"), 0644)

	k := kernel.New()
	if err := k.LoadConfig(cfgPath); err != nil {
		t.Fatalf("config: %v", err)
	}

	// Modules
	policyEngine := policy.NewEngine(policyPath)
	agentRuntime := agent.NewRuntime()
	agentRuntime.RegisterProvider(providers.NewMock("Hello from Cyntr!"))

	webhookAdapter := webhook.New("127.0.0.1:0")
	channelMgr := channel.NewManager()
	channelMgr.AddAdapter(webhookAdapter)

	if err := k.Register(policyEngine); err != nil {
		t.Fatalf("register policy: %v", err)
	}
	if err := k.Register(agentRuntime); err != nil {
		t.Fatalf("register agent: %v", err)
	}
	if err := k.Register(channelMgr); err != nil {
		t.Fatalf("register channel: %v", err)
	}

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer k.Stop(ctx)

	// Create an agent
	bus := k.Bus()
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: "assistant", Tenant: "marketing", Model: "mock",
			SystemPrompt: "You are helpful.", MaxTurns: 5,
		},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	// Send webhook message
	body := `{"tenant":"marketing","agent":"assistant","user_id":"U123","channel_id":"C456","text":"Hi there"}`
	resp, err := http.Post("http://"+webhookAdapter.Addr()+"/webhook", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("webhook: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)
	if result["response"] != "Hello from Cyntr!" {
		t.Fatalf("expected agent response, got %v", result)
	}
}
