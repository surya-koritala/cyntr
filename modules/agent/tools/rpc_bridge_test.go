package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type bridgeCapture struct {
	execName string
	execArgs map[string]string
	execHits int
	audits   []string // "tool:decision"
}

func testBridge(t *testing.T, policy func(tool string) string, cap *bridgeCapture) (*ToolRPCBridge, *httptest.Server) {
	t.Helper()
	b := newBridge("acme", "a1", "jane", &RPCConfig{
		PolicyCheck: func(_, _, _, tool string) string { return policy(tool) },
		Exec: func(_ context.Context, name string, args map[string]string) (string, error) {
			cap.execName, cap.execArgs, cap.execHits = name, args, cap.execHits+1
			return "RESULT:" + name, nil
		},
		Audit: func(_, _, _, tool, decision string) { cap.audits = append(cap.audits, tool+":"+decision) },
	})
	srv := httptest.NewServer(http.HandlerFunc(b.handler))
	t.Cleanup(srv.Close)
	return b, srv
}

func post(t *testing.T, url, token, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, bytes.NewBufferString(body))
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestBridgeExecutesAllowedTool(t *testing.T) {
	cap := &bridgeCapture{}
	b, srv := testBridge(t, func(string) string { return "allow" }, cap)
	code, out := post(t, srv.URL, b.token, `{"name":"file_read","args":{"path":"/tmp/x"}}`)
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if out["result"] != "RESULT:file_read" {
		t.Fatalf("unexpected result: %v", out)
	}
	if cap.execHits != 1 || cap.execName != "file_read" || cap.execArgs["path"] != "/tmp/x" {
		t.Fatalf("exec not called correctly: %+v", cap)
	}
	if len(cap.audits) != 1 || cap.audits[0] != "file_read:allow" {
		t.Fatalf("audit wrong: %v", cap.audits)
	}
}

func TestBridgePolicyDenyBlocksAndDoesNotExecute(t *testing.T) {
	cap := &bridgeCapture{}
	b, srv := testBridge(t, func(tool string) string {
		if tool == "shell_exec" {
			return "deny"
		}
		return "allow"
	}, cap)
	_, out := post(t, srv.URL, b.token, `{"name":"shell_exec","args":{"cmd":"rm -rf /"}}`)
	if out["error"] == nil || !strings.Contains(out["error"].(string), "deny") {
		t.Fatalf("denied tool should error: %v", out)
	}
	if cap.execHits != 0 {
		t.Fatal("denied tool must NOT execute — scripting is not a policy escape")
	}
	if len(cap.audits) != 1 || cap.audits[0] != "shell_exec:deny" {
		t.Fatalf("denied call should be audited: %v", cap.audits)
	}
}

func TestBridgeBlocksRecursiveTools(t *testing.T) {
	cap := &bridgeCapture{}
	b, srv := testBridge(t, func(string) string { return "allow" }, cap)
	for _, name := range []string{"code_interpreter", "orchestrate_agents", "delegate_agent"} {
		_, out := post(t, srv.URL, b.token, `{"name":"`+name+`","args":{}}`)
		if out["error"] == nil {
			t.Fatalf("%s should be blocked from scripts", name)
		}
	}
	if cap.execHits != 0 {
		t.Fatal("recursion-guarded tools must not execute")
	}
}

func TestBridgeRejectsBadToken(t *testing.T) {
	cap := &bridgeCapture{}
	_, srv := testBridge(t, func(string) string { return "allow" }, cap)
	code, _ := post(t, srv.URL, "Bearer wrong", `{"name":"file_read","args":{}}`)
	if code != http.StatusUnauthorized {
		t.Fatalf("bad token should be 401, got %d", code)
	}
	if cap.execHits != 0 {
		t.Fatal("unauthorized request must not execute")
	}
}

func TestRPCShimFor(t *testing.T) {
	if !strings.Contains(rpcShimFor("python"), "call_tool") {
		t.Fatal("python shim missing call_tool")
	}
	if !strings.Contains(rpcShimFor("javascript"), "call_tool") {
		t.Fatal("js shim missing call_tool")
	}
	if rpcShimFor("ruby") != "" {
		t.Fatal("unsupported language should have no shim")
	}
}
