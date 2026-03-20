package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func writePolicy(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return path
}

func TestRuleSetLoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := writePolicy(t, dir, `
rules:
  - name: allow-http
    tenant: "*"
    action: tool_call
    tool: http_request
    agent: "*"
    decision: allow
    priority: 10
  - name: deny-shell
    tenant: "*"
    action: tool_call
    tool: shell_exec
    agent: "*"
    decision: deny
    priority: 20
`)
	rs, err := LoadRuleSet(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(rs.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rs.Rules))
	}
	if rs.Rules[0].Name != "deny-shell" {
		t.Fatalf("expected highest priority first, got %q", rs.Rules[0].Name)
	}
}

func TestRuleSetLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := writePolicy(t, dir, `{{{invalid`)
	_, err := LoadRuleSet(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestRuleSetLoadInvalidDecision(t *testing.T) {
	dir := t.TempDir()
	path := writePolicy(t, dir, `
rules:
  - name: bad
    tenant: "*"
    action: "*"
    tool: "*"
    agent: "*"
    decision: maybe
    priority: 1
`)
	_, err := LoadRuleSet(path)
	if err == nil {
		t.Fatal("expected error for invalid decision")
	}
}

func TestRuleSetEvaluateDenyByDefault(t *testing.T) {
	rs := &RuleSet{Rules: []PolicyRule{}}
	resp := rs.Evaluate(CheckRequest{Tenant: "finance", Action: "tool_call", Tool: "shell_exec"})
	if resp.Decision != Deny {
		t.Fatalf("expected deny-by-default, got %s", resp.Decision)
	}
	if resp.Rule != "" {
		t.Fatalf("expected empty rule name, got %q", resp.Rule)
	}
}

func TestRuleSetEvaluateExactMatch(t *testing.T) {
	rs := &RuleSet{
		Rules: []PolicyRule{
			{Name: "allow-http", Tenant: "*", Action: "tool_call", Tool: "http_request", Agent: "*", Decision: Allow, Priority: 10},
		},
	}
	resp := rs.Evaluate(CheckRequest{Tenant: "marketing", Action: "tool_call", Tool: "http_request", Agent: "assistant"})
	if resp.Decision != Allow {
		t.Fatalf("expected allow, got %s", resp.Decision)
	}
	if resp.Rule != "allow-http" {
		t.Fatalf("expected rule 'allow-http', got %q", resp.Rule)
	}
}

func TestRuleSetEvaluateHigherPriorityWins(t *testing.T) {
	rs := &RuleSet{
		Rules: []PolicyRule{
			{Name: "deny-shell", Tenant: "*", Action: "tool_call", Tool: "shell_exec", Agent: "*", Decision: Deny, Priority: 20},
			{Name: "allow-all-tools", Tenant: "*", Action: "tool_call", Tool: "*", Agent: "*", Decision: Allow, Priority: 5},
		},
	}
	resp := rs.Evaluate(CheckRequest{Tenant: "finance", Action: "tool_call", Tool: "shell_exec"})
	if resp.Decision != Deny {
		t.Fatalf("expected deny (higher priority), got %s", resp.Decision)
	}
}

func TestRuleSetEvaluateTenantSpecific(t *testing.T) {
	rs := &RuleSet{
		Rules: []PolicyRule{
			{Name: "finance-deny-shell", Tenant: "finance", Action: "tool_call", Tool: "shell_exec", Agent: "*", Decision: Deny, Priority: 30},
			{Name: "allow-shell", Tenant: "*", Action: "tool_call", Tool: "shell_exec", Agent: "*", Decision: Allow, Priority: 10},
		},
	}
	resp := rs.Evaluate(CheckRequest{Tenant: "finance", Action: "tool_call", Tool: "shell_exec"})
	if resp.Decision != Deny {
		t.Fatalf("finance should be denied, got %s", resp.Decision)
	}
	resp = rs.Evaluate(CheckRequest{Tenant: "marketing", Action: "tool_call", Tool: "shell_exec"})
	if resp.Decision != Allow {
		t.Fatalf("marketing should be allowed, got %s", resp.Decision)
	}
}
