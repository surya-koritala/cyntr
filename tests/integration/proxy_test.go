package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/modules/policy"
	"github.com/cyntr-dev/cyntr/modules/proxy"
)

func TestProxyGatewayPolicyEnforcement(t *testing.T) {
	dir := t.TempDir()

	policyPath := filepath.Join(dir, "policy.yaml")
	os.WriteFile(policyPath, []byte(`
rules:
  - name: deny-shell
    tenant: finance
    action: "*"
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 20
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

	policyEngine := policy.NewEngine(policyPath)
	gateway := proxy.NewGateway("127.0.0.1:0")

	if err := k.Register(policyEngine); err != nil {
		t.Fatalf("register policy: %v", err)
	}
	if err := k.Register(gateway); err != nil {
		t.Fatalf("register proxy: %v", err)
	}

	ctx := t.Context()
	if err := k.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer k.Stop(ctx)

	time.Sleep(200 * time.Millisecond)

	addr := gateway.Addr()

	// Test 1: Allowed request (marketing tenant, model call)
	req, _ := http.NewRequest("POST", "http://"+addr+"/v1/messages",
		strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("X-Cyntr-Tenant", "marketing")
	req.Header.Set("Content-Type", "application/json")

	// Note: this will fail to proxy (no upstream), but it should get past policy
	// The upstream failure gives us a 502, not a 403
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp.Body.Close()
	// 502 means policy allowed but upstream failed (expected — no real upstream)
	if resp.StatusCode == http.StatusForbidden {
		t.Fatal("marketing should not be denied")
	}

	// Test 2: Health endpoint
	resp, err = http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var health map[string]string
	json.NewDecoder(resp.Body).Decode(&health)
	if health["status"] != "ok" {
		t.Fatalf("expected ok, got %v", health)
	}
}
