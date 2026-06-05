package agent

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func TestSandboxActive(t *testing.T) {
	cases := []struct {
		mode, name string
		want       bool
	}{
		{SandboxOff, "x", false},
		{"", "x", false},
		{SandboxAlways, "x", true},
		{SandboxAlways, "main", true},
		{SandboxNonMain, "main", false},
		{SandboxNonMain, "worker", true},
	}
	for _, c := range cases {
		cfg := AgentConfig{Name: c.name, Sandbox: SandboxConfig{Mode: c.mode}}
		if got := cfg.SandboxActive(); got != c.want {
			t.Fatalf("mode=%q name=%q: SandboxActive=%v want %v", c.mode, c.name, got, c.want)
		}
	}
}

func TestSafeToolsetStripsUnsafe(t *testing.T) {
	requested := []string{"file_read", "recall_search", "http_request", "kubectl", "shell_exec", "code_interpreter", "json_query"}

	// No docker backend: network/host tools AND host code-exec removed.
	got := SafeToolset(requested, "")
	want := []string{"file_read", "json_query", "recall_search"}
	if !sameSet(got, want) {
		t.Fatalf("host backend sandbox toolset = %v, want %v", got, want)
	}
	for _, banned := range []string{"http_request", "kubectl", "shell_exec", "code_interpreter"} {
		if hasName(got, banned) {
			t.Fatalf("%s must not be in a host-backed sandbox", banned)
		}
	}
}

func TestSafeToolsetKeepsHostExecWithDocker(t *testing.T) {
	requested := []string{"shell_exec", "code_interpreter", "http_request", "file_read"}
	got := SafeToolset(requested, "docker")
	// Containerized: shell/code stay; network still stripped.
	if !hasName(got, "shell_exec") || !hasName(got, "code_interpreter") {
		t.Fatalf("docker backend should keep containerized code-exec: %v", got)
	}
	if hasName(got, "http_request") {
		t.Fatalf("network tools are stripped even with docker: %v", got)
	}
}

// toolRecordingProvider records the tool defs it was handed.
type toolRecordingProvider struct{ tools []ToolDef }

func (p *toolRecordingProvider) Name() string { return "rec" }
func (p *toolRecordingProvider) Chat(ctx context.Context, msgs []Message, tools []ToolDef) (Message, error) {
	p.tools = tools
	return Message{Role: RoleAssistant, Content: "ok"}, nil
}

// namedTool is a minimal Tool used to populate the registry in tests.
type namedTool struct{ name string }

func (n *namedTool) Name() string                     { return n.name }
func (n *namedTool) Description() string              { return n.name }
func (n *namedTool) Parameters() map[string]ToolParam { return nil }
func (n *namedTool) Execute(context.Context, map[string]string) (string, error) {
	return "", nil
}

func TestSandboxedAgentNeverSeesUnsafeTools(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	prov := &toolRecordingProvider{}
	reg := NewToolRegistry()
	for _, n := range []string{"shell_exec", "http_request", "recall_search", "file_read"} {
		reg.Register(&namedTool{name: n})
	}
	rt := NewRuntime()
	rt.RegisterProvider(prov)
	rt.SetToolRegistry(reg)
	ctx := context.Background()
	rt.Init(ctx, &kernel.Services{Bus: bus})
	rt.Start(ctx)
	defer rt.Stop(ctx)

	bus.Request(ctx, ipc.Message{Source: "t", Target: "agent_runtime", Topic: "agent.create",
		Payload: AgentConfig{
			Name: "untrusted", Tenant: "acme", Model: "rec", MaxTurns: 2,
			Tools:   []string{"shell_exec", "http_request", "recall_search", "file_read"},
			Sandbox: SandboxConfig{Mode: SandboxAlways},
		}})

	bus.Request(ctx, ipc.Message{Source: "t", Target: "agent_runtime", Topic: "agent.chat",
		Payload: ChatRequest{Agent: "untrusted", Tenant: "acme", User: "jane", Message: "hi"}})

	var names []string
	for _, d := range prov.tools {
		names = append(names, d.Name)
	}
	if hasName(names, "shell_exec") || hasName(names, "http_request") {
		t.Fatalf("sandboxed agent was offered unsafe tools: %v", names)
	}
	if !hasName(names, "recall_search") || !hasName(names, "file_read") {
		t.Fatalf("sandboxed agent lost its safe tools: %v", names)
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	return strings.Join(ac, ",") == strings.Join(bc, ",")
}

func hasName(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
