package tools

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// TestCodeInterpreterRPCRoundTrip exercises the full path: a real Python
// script calls cyntr.call_tool, which round-trips through the loopback bridge
// back into Go. Skipped when python3 isn't installed.
func TestCodeInterpreterRPCRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}

	var called string
	tool := NewCodeInterpreterTool()
	tool.EnableRPC(&RPCConfig{
		PolicyCheck: func(_, _, _, name string) string {
			if name == "forbidden" {
				return "deny"
			}
			return "allow"
		},
		Exec: func(_ context.Context, name string, args map[string]string) (string, error) {
			called = name
			return "TOOLRESULT:" + args["text"], nil
		},
		Audit: func(_, _, _, _, _ string) {},
	})
	ctx := agent.WithToolCaller(context.Background(), "acme", "a1", "jane")

	// Allowed tool call from inside the script.
	out, err := tool.Execute(ctx, map[string]string{
		"language": "python",
		"code":     `print(cyntr.call_tool("echo", text="hi"))`,
	})
	if err != nil {
		t.Fatalf("execute: %v (out=%s)", err, out)
	}
	if !strings.Contains(out, "TOOLRESULT:hi") {
		t.Fatalf("round-trip failed: %q", out)
	}
	if called != "echo" {
		t.Fatalf("tool not invoked, got %q", called)
	}

	// A policy-denied tool must raise inside the script (no escape).
	out2, _ := tool.Execute(ctx, map[string]string{
		"language": "python",
		"code": `
try:
    cyntr.call_tool("forbidden")
    print("NOT_BLOCKED")
except Exception:
    print("BLOCKED")
`,
	})
	if !strings.Contains(out2, "BLOCKED") || strings.Contains(out2, "NOT_BLOCKED") {
		t.Fatalf("policy denial not enforced in-script: %q", out2)
	}
}
