package agent

// Per-session sandboxing (C15).
//
// Cyntr already isolates *where* shell runs (the per-tenant Docker shell
// backend). This complements that by isolating *what an untrusted session can
// reach*: when a session is sandboxed, its tool surface is intersected with a
// safe set. Network- and host-reaching tools are removed entirely, and host
// code-execution tools (shell/code interpreter) are removed unless the agent
// declares a containerized backend. Policy decides *whether* a tool call is
// allowed; the sandbox decides *which tools even exist* for the session.

// Sandbox modes.
const (
	SandboxOff     = "off"
	SandboxNonMain = "non-main"
	SandboxAlways  = "always"
)

// sandboxUnsafeTools are never available to a sandboxed session: they reach
// the network, the host, external services, or fan out to other agents.
var sandboxUnsafeTools = map[string]bool{
	"browse_web":         true,
	"advanced_browse":    true,
	"chromium_browser":   true,
	"web_search":         true,
	"http_request":       true,
	"kubectl":            true,
	"aws_cross_account":  true,
	"aws_cost_explorer":  true,
	"github":             true,
	"jira":               true,
	"send_message":       true,
	"send_notification":  true,
	"generate_image":     true,
	"database_query":     true,
	"orchestrate_agents": true,
	"delegate_agent":     true,
}

// sandboxHostExecTools run arbitrary code; in a sandbox they are only allowed
// when a containerized backend is declared (Backend == "docker").
var sandboxHostExecTools = map[string]bool{
	"shell_exec":       true,
	"code_interpreter": true,
}

// SandboxActive reports whether this agent's sessions should be sandboxed.
func (c AgentConfig) SandboxActive() bool {
	switch c.Sandbox.Mode {
	case SandboxAlways:
		return true
	case SandboxNonMain:
		// Everything except the trusted "main" agent is sandboxed.
		return c.Name != "main"
	default: // "off" or unset
		return false
	}
}

// SafeToolset returns the subset of requested tools allowed in a sandbox:
// always drop network/host-reaching tools, and drop host code-execution tools
// unless backend is "docker" (containerized).
func SafeToolset(requested []string, backend string) []string {
	out := make([]string, 0, len(requested))
	for _, name := range requested {
		if sandboxUnsafeTools[name] {
			continue
		}
		if sandboxHostExecTools[name] && backend != "docker" {
			continue
		}
		out = append(out, name)
	}
	return out
}
