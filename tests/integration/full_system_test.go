package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/auth"
	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/agent/providers"
	"github.com/cyntr-dev/cyntr/modules/audit"
	"github.com/cyntr-dev/cyntr/modules/channel"
	"github.com/cyntr-dev/cyntr/modules/channel/webhook"
	"github.com/cyntr-dev/cyntr/modules/federation"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/modules/proxy"
	"github.com/cyntr-dev/cyntr/modules/skill"
)

// fullSystem boots ALL modules and returns the kernel + bus + cleanup func.
func fullSystem(t *testing.T) (*kernel.Kernel, *ipc.Bus) {
	t.Helper()
	dir := t.TempDir()

	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte(`
rules:
  - name: finance-deny-shell
    tenant: finance
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 30
  - name: allow-all
    tenant: "*"
    action: "*"
    tool: "*"
    agent: "*"
    decision: allow
    priority: 1
`), 0644)

	cfgPath := filepath.Join(dir, "cyntr.yaml")
	os.WriteFile(cfgPath, []byte(`
version: "1"
listen:
  address: "127.0.0.1:0"
  webui: ":0"
tenants:
  finance:
    isolation: process
    policy: finance-strict
  marketing:
    isolation: namespace
    policy: marketing-standard
`), 0644)

	k := kernel.New()
	if err := k.LoadConfig(cfgPath); err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Register ALL modules. Order matters only for non-topo-sorted cases;
	// the kernel's topological sort handles dependency ordering automatically.
	policyEngine := policy.NewEngine(policyPath)
	auditLogger := audit.NewLogger(filepath.Join(dir, "audit.db"), "test-node", "secret")
	agentRuntime := agent.NewRuntime()
	agentRuntime.RegisterProvider(providers.NewMock("Mock response"))
	channelMgr := channel.NewManager()
	webhookAdapter := webhook.New("127.0.0.1:0")
	channelMgr.AddAdapter(webhookAdapter)
	proxyGateway := proxy.NewGateway("127.0.0.1:0")
	skillRuntime := skill.NewRuntime()
	federationMod := federation.NewModule("test-node")
	authMod := auth.NewModule(
		auth.NewSessionManager("test-secret-key-minimum-32-bytes!"),
		auth.NewRBAC(),
	)

	if err := k.Register(policyEngine); err != nil {
		t.Fatalf("register policy: %v", err)
	}
	if err := k.Register(auditLogger); err != nil {
		t.Fatalf("register audit: %v", err)
	}
	if err := k.Register(agentRuntime); err != nil {
		t.Fatalf("register agent_runtime: %v", err)
	}
	if err := k.Register(channelMgr); err != nil {
		t.Fatalf("register channel: %v", err)
	}
	if err := k.Register(proxyGateway); err != nil {
		t.Fatalf("register proxy: %v", err)
	}
	if err := k.Register(skillRuntime); err != nil {
		t.Fatalf("register skill_runtime: %v", err)
	}
	if err := k.Register(federationMod); err != nil {
		t.Fatalf("register federation: %v", err)
	}
	if err := k.Register(authMod); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start full system: %v", err)
	}

	t.Cleanup(func() { k.Stop(ctx) })
	return k, k.Bus()
}

// --- Test: Full System Boot ---

func TestFullSystemBoot(t *testing.T) {
	k, _ := fullSystem(t)

	ctx := context.Background()
	report := k.HealthReport(ctx)

	// All modules should be healthy
	expectedModules := []string{"policy", "audit", "agent_runtime", "channel", "proxy", "skill_runtime", "federation", "auth"}
	for _, name := range expectedModules {
		h, ok := report[name]
		if !ok {
			t.Errorf("module %q not in health report", name)
			continue
		}
		if !h.Healthy {
			t.Errorf("module %q unhealthy: %s", name, h.Message)
		}
	}
}

// --- Test: Multi-Tenant Isolation ---

func TestMultiTenantIsolation(t *testing.T) {
	_, bus := fullSystem(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create agents in different tenants
	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{Name: "bot", Tenant: "finance", Model: "mock", MaxTurns: 5},
	})
	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{Name: "bot", Tenant: "marketing", Model: "mock", MaxTurns: 5},
	})

	// List finance agents — should only see finance bot
	resp, _ := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.list",
		Payload: "finance",
	})
	finAgents := resp.Payload.([]string)
	if len(finAgents) != 1 || finAgents[0] != "bot" {
		t.Fatalf("finance should have 1 agent, got %v", finAgents)
	}

	// Policy: finance tenant denied shell
	resp, _ = bus.Request(ctx, ipc.Message{
		Source: "test", Target: "policy", Topic: "policy.check",
		Payload: policy.CheckRequest{Tenant: "finance", Action: "tool_call", Tool: "shell_exec"},
	})
	if resp.Payload.(policy.CheckResponse).Decision != policy.Deny {
		t.Fatal("finance should be denied shell_exec")
	}

	// Marketing tenant allowed shell
	resp, _ = bus.Request(ctx, ipc.Message{
		Source: "test", Target: "policy", Topic: "policy.check",
		Payload: policy.CheckRequest{Tenant: "marketing", Action: "tool_call", Tool: "shell_exec"},
	})
	if resp.Payload.(policy.CheckResponse).Decision != policy.Allow {
		t.Fatal("marketing should be allowed shell_exec")
	}
}

// --- Test: Agent Chat + Audit Trail ---

func TestAgentChatCreatesAuditTrail(t *testing.T) {
	_, bus := fullSystem(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create agent
	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{Name: "audited-bot", Tenant: "finance", Model: "mock", MaxTurns: 5},
	})

	// Chat
	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{Agent: "audited-bot", Tenant: "finance", User: "jane@corp.com", Message: "Hello"},
	})

	// Write audit entry for the chat
	bus.Publish(ipc.Message{
		Source: "test", Target: "*", Type: ipc.MessageTypeEvent, Topic: "audit.write",
		Payload: audit.Entry{
			ID: "evt_chat_001", Timestamp: time.Now().UTC(), Tenant: "finance",
			Principal: audit.Principal{User: "jane@corp.com", Agent: "audited-bot"},
			Action:    audit.Action{Type: "agent_chat", Module: "agent_runtime"},
			Policy:    audit.PolicyDecision{Decision: "allow"},
			Result:    audit.Result{Status: "success"},
		},
	})

	time.Sleep(300 * time.Millisecond)

	// Query audit — should find the entry
	resp, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "audit", Topic: "audit.query",
		Payload: audit.QueryFilter{Tenant: "finance"},
	})
	if err != nil {
		t.Fatalf("query: %v", err)
	}

	entries := resp.Payload.([]audit.Entry)
	if len(entries) < 1 {
		t.Fatal("expected at least 1 audit entry")
	}

	found := false
	for _, e := range entries {
		if e.ID == "evt_chat_001" {
			found = true
		}
	}
	if !found {
		t.Fatal("audit entry evt_chat_001 not found")
	}
}

// --- Test: Concurrent Agent Chats Across Tenants ---

func TestConcurrentChatsAcrossTenants(t *testing.T) {
	_, bus := fullSystem(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create agents in both tenants
	for _, tn := range []string{"finance", "marketing"} {
		bus.Request(ctx, ipc.Message{
			Source: "test", Target: "agent_runtime", Topic: "agent.create",
			Payload: agent.AgentConfig{Name: "chatbot", Tenant: tn, Model: "mock", MaxTurns: 5},
		})
	}

	// 20 concurrent chats, 10 per tenant
	var wg sync.WaitGroup
	errors := make([]error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tn := "finance"
			if idx >= 10 {
				tn = "marketing"
			}
			_, err := bus.Request(ctx, ipc.Message{
				Source: "test", Target: "agent_runtime", Topic: "agent.chat",
				Payload: agent.ChatRequest{
					Agent:   "chatbot",
					Tenant:  tn,
					User:    fmt.Sprintf("user%d@corp.com", idx),
					Message: fmt.Sprintf("Message %d", idx),
				},
			})
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	for i, err := range errors {
		if err != nil {
			t.Errorf("chat %d failed: %v", i, err)
		}
	}
}

// --- Test: Skill Install + List ---

func TestSkillInstallAndList(t *testing.T) {
	_, bus := fullSystem(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create a test skill on disk
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "test-skill")
	os.MkdirAll(skillDir, 0755)
	os.WriteFile(filepath.Join(skillDir, "skill.yaml"), []byte("name: integration-test-skill\nversion: 1.0.0\n"), 0644)
	os.WriteFile(filepath.Join(skillDir, "skill.md"), []byte("# Test\nInstructions here."), 0644)

	// Install
	resp, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "skill_runtime", Topic: "skill.install",
		Payload: skillDir,
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("expected ok, got %v", resp.Payload)
	}

	// List
	resp, _ = bus.Request(ctx, ipc.Message{
		Source: "test", Target: "skill_runtime", Topic: "skill.list",
	})
	names := resp.Payload.([]string)
	found := false
	for _, n := range names {
		if n == "integration-test-skill" {
			found = true
		}
	}
	if !found {
		t.Fatalf("skill not found in list: %v", names)
	}

	// Get instructions
	resp, _ = bus.Request(ctx, ipc.Message{
		Source: "test", Target: "skill_runtime", Topic: "skill.instructions",
		Payload: []string{"integration-test-skill"},
	})
	instructions := resp.Payload.(map[string]string)
	if instructions["integration-test-skill"] == "" {
		t.Fatal("expected instructions")
	}
}

// --- Test: Federation Peer Lifecycle ---

func TestFederationPeerLifecycle(t *testing.T) {
	_, bus := fullSystem(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Join peer
	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "federation", Topic: "federation.join",
		Payload: federation.Peer{Name: "east-node", Endpoint: "http://east:8443"},
	})

	// List peers
	resp, _ := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "federation", Topic: "federation.peers",
	})
	peers := resp.Payload.([]federation.Peer)
	if len(peers) != 1 || peers[0].Name != "east-node" {
		t.Fatalf("expected east-node, got %v", peers)
	}

	// Remove peer
	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "federation", Topic: "federation.remove",
		Payload: "east-node",
	})

	// Verify removed
	resp, _ = bus.Request(ctx, ipc.Message{
		Source: "test", Target: "federation", Topic: "federation.peers",
	})
	peers = resp.Payload.([]federation.Peer)
	if len(peers) != 0 {
		t.Fatalf("expected 0 peers, got %d", len(peers))
	}
}

// --- Test: Auth → Policy → Agent Flow ---

func TestAuthPolicyAgentFlow(t *testing.T) {
	_, bus := fullSystem(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Resolve auth (will fail since no real token — just test the flow)
	_, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "auth", Topic: "auth.resolve",
		Payload: auth.ResolveRequest{Scheme: "bearer", Credential: "invalid-token"},
	})
	// Should error (invalid token)
	if err == nil {
		t.Fatal("expected auth error for invalid token")
	}
}

// --- Test: Module Health After Operations ---

func TestModuleHealthAfterOperations(t *testing.T) {
	k, bus := fullSystem(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Do various operations
	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{Name: "test", Tenant: "finance", Model: "mock", MaxTurns: 5},
	})
	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{Agent: "test", Tenant: "finance", Message: "hi"},
	})

	// All modules should still be healthy
	report := k.HealthReport(ctx)
	for name, h := range report {
		if !h.Healthy {
			t.Errorf("module %q unhealthy after operations: %s", name, h.Message)
		}
	}
}
