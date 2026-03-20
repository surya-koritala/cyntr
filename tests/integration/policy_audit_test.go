package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/audit"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

func TestPolicyCheckAndAuditLog(t *testing.T) {
	dir := t.TempDir()

	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte(`
rules:
  - name: allow-http
    tenant: "*"
    action: tool_call
    tool: http_request
    agent: "*"
    decision: allow
    priority: 10
`), 0644)

	cfgPath := filepath.Join(dir, "cyntr.yaml")
	os.WriteFile(cfgPath, []byte("version: \"1\"\nlisten:\n  address: \"127.0.0.1:8080\"\n"), 0644)

	k := kernel.New()
	if err := k.LoadConfig(cfgPath); err != nil {
		t.Fatalf("load config: %v", err)
	}

	policyEngine := policy.NewEngine(policyPath)
	auditLogger := audit.NewLogger(filepath.Join(dir, "audit.db"), "test-instance", "test-secret")

	if err := k.Register(policyEngine); err != nil {
		t.Fatalf("register policy: %v", err)
	}
	if err := k.Register(auditLogger); err != nil {
		t.Fatalf("register audit: %v", err)
	}

	ctx := context.Background()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer k.Stop(ctx)

	bus := k.Bus()

	// Step 1: Check policy
	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	policyResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "proxy", Target: "policy", Topic: "policy.check",
		Payload: policy.CheckRequest{
			Tenant: "marketing", Action: "tool_call", Tool: "http_request",
			Agent: "assistant", User: "alice@corp.com",
		},
	})
	if err != nil {
		t.Fatalf("policy check: %v", err)
	}

	checkResp := policyResp.Payload.(policy.CheckResponse)
	if checkResp.Decision != policy.Allow {
		t.Fatalf("expected allow, got %s", checkResp.Decision)
	}

	// Step 2: Log the decision to audit
	bus.Publish(ipc.Message{
		Source: "proxy", Target: "*", Type: ipc.MessageTypeEvent, Topic: "audit.write",
		Payload: audit.Entry{
			ID: "evt_integration_001", Timestamp: time.Now().UTC(), Tenant: "marketing",
			Principal: audit.Principal{User: "alice@corp.com", Agent: "assistant"},
			Action:    audit.Action{Type: "tool_call", Detail: map[string]string{"tool": "http_request"}},
			Policy:    audit.PolicyDecision{Rule: checkResp.Rule, Decision: checkResp.Decision.String(), DecidedBy: "policy_engine"},
			Result:    audit.Result{Status: "success"},
		},
	})

	time.Sleep(300 * time.Millisecond)

	// Step 3: Query audit log
	auditResp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "audit", Topic: "audit.query",
		Payload: audit.QueryFilter{Tenant: "marketing"},
	})
	if err != nil {
		t.Fatalf("audit query: %v", err)
	}

	entries := auditResp.Payload.([]audit.Entry)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0].Policy.Decision != "allow" {
		t.Fatalf("expected 'allow' in audit, got %q", entries[0].Policy.Decision)
	}
	if entries[0].Principal.User != "alice@corp.com" {
		t.Fatalf("expected alice, got %q", entries[0].Principal.User)
	}
}
