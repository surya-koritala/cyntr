package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeRego(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write rego: %v", err)
	}
	return path
}

func TestLoadRegoPolicyEmptyPath(t *testing.T) {
	ev, err := LoadRegoPolicy("")
	if err != nil {
		t.Fatalf("empty path should not error: %v", err)
	}
	if ev != nil {
		t.Fatal("empty path should return nil evaluator (disabled)")
	}
}

func TestLoadRegoPolicyNonexistentPath(t *testing.T) {
	ev, err := LoadRegoPolicy("/nonexistent/policy.rego")
	if err != nil {
		t.Fatalf("nonexistent path should not error: %v", err)
	}
	if ev != nil {
		t.Fatal("nonexistent path should return nil evaluator (disabled)")
	}
}

func TestLoadRegoPolicyEmptyDir(t *testing.T) {
	dir := t.TempDir()
	ev, err := LoadRegoPolicy(dir)
	if err != nil {
		t.Fatalf("empty dir should not error: %v", err)
	}
	if ev != nil {
		t.Fatal("empty dir (no .rego files) should return nil evaluator")
	}
}

func TestLoadRegoPolicyValidFile(t *testing.T) {
	dir := t.TempDir()
	path := writeRego(t, dir, "policy.rego", `package cyntr.policy

default decision := "allow"
`)
	ev, err := LoadRegoPolicy(path)
	if err != nil {
		t.Fatalf("load valid rego: %v", err)
	}
	if ev == nil {
		t.Fatal("expected non-nil evaluator")
	}
	if len(ev.Sources()) != 1 {
		t.Fatalf("expected 1 source file, got %d", len(ev.Sources()))
	}
}

func TestLoadRegoPolicyValidDir(t *testing.T) {
	dir := t.TempDir()
	writeRego(t, dir, "a.rego", `package cyntr.policy

default decision := "allow"
`)
	writeRego(t, dir, "b.rego", `package cyntr.policy

decision := "deny" if {
  input.tool == "shell_exec"
}
`)
	// Throw in a non-rego file to confirm filtering.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("ignore me"), 0644); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	ev, err := LoadRegoPolicy(dir)
	if err != nil {
		t.Fatalf("load dir: %v", err)
	}
	if ev == nil {
		t.Fatal("expected non-nil evaluator")
	}
	if len(ev.Sources()) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(ev.Sources()))
	}
}

func TestLoadRegoPolicyInvalidSyntax(t *testing.T) {
	dir := t.TempDir()
	path := writeRego(t, dir, "bad.rego", `this is not valid rego ::: !!!`)
	ev, err := LoadRegoPolicy(path)
	if err == nil {
		t.Fatal("expected error for invalid rego syntax")
	}
	if ev != nil {
		t.Fatal("invalid rego should return nil evaluator on error")
	}
}

func TestRegoEvaluateAllow(t *testing.T) {
	dir := t.TempDir()
	path := writeRego(t, dir, "policy.rego", `package cyntr.policy

default decision := "allow"
`)
	ev, err := LoadRegoPolicy(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	resp := ev.Evaluate(context.Background(), CheckRequest{
		Tenant: "marketing", Action: "tool_call", Tool: "http_request", Agent: "assistant",
	})
	if resp.Decision != Allow {
		t.Fatalf("expected allow, got %s — reason=%q", resp.Decision, resp.Reason)
	}
}

func TestRegoEvaluateDeny(t *testing.T) {
	dir := t.TempDir()
	path := writeRego(t, dir, "policy.rego", `package cyntr.policy

default decision := "allow"

decision := "deny" if {
  input.tool == "shell_exec"
  startswith(input.tenant, "prod-")
}
`)
	ev, err := LoadRegoPolicy(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	resp := ev.Evaluate(context.Background(), CheckRequest{
		Tenant: "prod-finance", Action: "tool_call", Tool: "shell_exec", Agent: "assistant",
	})
	if resp.Decision != Deny {
		t.Fatalf("expected deny, got %s — reason=%q", resp.Decision, resp.Reason)
	}

	// Same tool but non-prod tenant should fall through to default allow.
	resp = ev.Evaluate(context.Background(), CheckRequest{
		Tenant: "dev-finance", Action: "tool_call", Tool: "shell_exec", Agent: "assistant",
	})
	if resp.Decision != Allow {
		t.Fatalf("expected allow for non-prod tenant, got %s", resp.Decision)
	}
}

func TestRegoEvaluateRequireApproval(t *testing.T) {
	dir := t.TempDir()
	path := writeRego(t, dir, "policy.rego", `package cyntr.policy

default decision := "allow"

decision := "require_approval" if {
  input.action == "tool_call"
  startswith(input.tool, "aws")
}
`)
	ev, err := LoadRegoPolicy(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	resp := ev.Evaluate(context.Background(), CheckRequest{
		Tenant: "marketing", Action: "tool_call", Tool: "aws_s3_put", Agent: "assistant",
	})
	if resp.Decision != RequireApproval {
		t.Fatalf("expected require_approval, got %s — reason=%q", resp.Decision, resp.Reason)
	}
}

func TestRegoEvaluateUnrecognizedDecisionFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := writeRego(t, dir, "policy.rego", `package cyntr.policy

default decision := "maybe"
`)
	ev, err := LoadRegoPolicy(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	resp := ev.Evaluate(context.Background(), CheckRequest{Tenant: "x", Action: "y", Tool: "z"})
	if resp.Decision != Deny {
		t.Fatalf("unknown decision should fail closed to deny, got %s", resp.Decision)
	}
}
