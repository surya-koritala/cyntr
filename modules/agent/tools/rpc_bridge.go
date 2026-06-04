package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// ToolRPCBridge lets a code_interpreter script call back into Cyntr's tools in
// the same turn (E21) — `cyntr.call_tool(name, args)` round-trips over a
// loopback HTTP endpoint. Every call is re-checked against policy for the
// calling tenant/agent and audited, so a script can NEVER reach a tool the
// agent itself couldn't call: scripting is not a policy escape hatch.
type ToolRPCBridge struct {
	token     string
	tenant    string
	agentName string
	user      string

	// Injected so the bridge is testable without a real registry/bus.
	policyCheck func(tenant, user, agent, tool string) string // "allow" | "deny" | ...
	exec        func(ctx context.Context, name string, args map[string]string) (string, error)
	audit       func(tenant, user, agent, tool, decision string)

	// denied tools can never be invoked from a script (recursion / fan-out
	// guards): the interpreter itself and the multi-agent tools.
	denied map[string]bool
}

// RPCConfig carries the wiring a code tool needs to expose the bridge.
type RPCConfig struct {
	PolicyCheck func(tenant, user, agent, tool string) string
	Exec        func(ctx context.Context, name string, args map[string]string) (string, error)
	Audit       func(tenant, user, agent, tool, decision string)
}

func newBridge(tenant, agentName, user string, cfg *RPCConfig) *ToolRPCBridge {
	return &ToolRPCBridge{
		token:       "Bearer " + randToken(),
		tenant:      tenant,
		agentName:   agentName,
		user:        user,
		policyCheck: cfg.PolicyCheck,
		exec:        cfg.Exec,
		audit:       cfg.Audit,
		denied: map[string]bool{
			"code_interpreter":   true,
			"orchestrate_agents": true,
			"delegate_agent":     true,
		},
	}
}

type rpcRequest struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

func (b *ToolRPCBridge) handler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != b.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSON(w, map[string]any{"error": "invalid request"})
		return
	}
	if b.denied[req.Name] {
		writeJSON(w, map[string]any{"error": "tool not callable from a script: " + req.Name})
		return
	}

	decision := "allow"
	if b.policyCheck != nil {
		decision = b.policyCheck(b.tenant, b.user, b.agentName, req.Name)
	}
	if decision != "allow" {
		if b.audit != nil {
			b.audit(b.tenant, b.user, b.agentName, req.Name, decision)
		}
		writeJSON(w, map[string]any{"error": fmt.Sprintf("policy %s tool: %s", decision, req.Name)})
		return
	}

	args := make(map[string]string, len(req.Args))
	for k, v := range req.Args {
		args[k] = fmt.Sprint(v)
	}
	out, err := b.exec(r.Context(), req.Name, args)
	if b.audit != nil {
		b.audit(b.tenant, b.user, b.agentName, req.Name, decision)
	}
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"result": out})
}

// serve starts the bridge on a loopback port and returns its URL plus a stop
// function. Only 127.0.0.1 is bound, so nothing off-host can reach it.
func (b *ToolRPCBridge) serve() (string, func(), error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: http.HandlerFunc(b.handler), ReadHeaderTimeout: 5 * time.Second}
	go srv.Serve(ln)
	url := fmt.Sprintf("http://%s/", ln.Addr().String())
	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}
	return url, stop, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func randToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "cyntr-rpc"
	}
	return hex.EncodeToString(buf)
}

// pythonRPCShim is prepended to a Python script so `cyntr.call_tool(name, ...)`
// works. URL + token come from the environment, never baked into the code.
const pythonRPCShim = `import os as _os, json as _json, urllib.request as _u
def _cyntr_call(_name, _args):
    _req = _u.Request(_os.environ["CYNTR_RPC_URL"], data=_json.dumps({"name": _name, "args": _args}).encode(),
                      headers={"Authorization": _os.environ["CYNTR_RPC_TOKEN"], "Content-Type": "application/json"})
    _resp = _json.load(_u.urlopen(_req))
    if "error" in _resp:
        raise RuntimeError(_resp["error"])
    return _resp.get("result", "")
class _Cyntr:
    def call_tool(self, name, **args):
        return _cyntr_call(name, args)
cyntr = _Cyntr()
`

// jsRPCShim is the Node.js equivalent (Node 18+ provides global fetch).
const jsRPCShim = `globalThis.cyntr = {
  call_tool: async (name, args = {}) => {
    const r = await fetch(process.env.CYNTR_RPC_URL, {
      method: "POST",
      headers: { "Authorization": process.env.CYNTR_RPC_TOKEN, "Content-Type": "application/json" },
      body: JSON.stringify({ name, args }),
    });
    const j = await r.json();
    if (j.error) throw new Error(j.error);
    return j.result;
  },
};
`

// rpcShimFor returns the shim for a language, or "" if unsupported.
func rpcShimFor(lang string) string {
	switch lang {
	case "python", "python3":
		return pythonRPCShim
	case "javascript", "js", "node":
		return jsRPCShim
	default:
		return ""
	}
}
