package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

// stubTool is a minimal Tool used to exercise the registry. The Execute func
// is closed over by the test so each test can control behavior.
type stubTool struct {
	name string
	exec func(ctx context.Context, in map[string]string) (string, error)
}

func (s *stubTool) Name() string                          { return s.name }
func (s *stubTool) Description() string                   { return "stub" }
func (s *stubTool) Parameters() map[string]agent.ToolParam { return nil }
func (s *stubTool) Execute(ctx context.Context, in map[string]string) (string, error) {
	return s.exec(ctx, in)
}

func newPlanFixture(tools ...*stubTool) (*agent.ToolRegistry, *ToolPlanTool) {
	reg := agent.NewToolRegistry()
	for _, t := range tools {
		reg.Register(t)
	}
	plan := NewToolPlanTool(reg, nil) // nil bus = no policy
	reg.Register(plan)
	return reg, plan
}

func TestToolPlanName(t *testing.T) {
	if NewToolPlanTool(agent.NewToolRegistry(), nil).Name() != "tool_plan" {
		t.Fatal("wrong name")
	}
}

func TestToolPlanSingleStep(t *testing.T) {
	echo := &stubTool{name: "echo", exec: func(_ context.Context, in map[string]string) (string, error) {
		return in["text"], nil
	}}
	_, plan := newPlanFixture(echo)

	out, err := plan.Execute(context.Background(), map[string]string{
		"plan": `{"steps":[{"id":"s1","tool":"echo","params":{"text":"hello"}}]}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"final": "hello"`) || !strings.Contains(out, `"s1": "hello"`) {
		t.Fatalf("unexpected result: %s", out)
	}
}

func TestToolPlanChainedSteps(t *testing.T) {
	upper := &stubTool{name: "upper", exec: func(_ context.Context, in map[string]string) (string, error) {
		return strings.ToUpper(in["text"]), nil
	}}
	wrap := &stubTool{name: "wrap", exec: func(_ context.Context, in map[string]string) (string, error) {
		return "<" + in["text"] + ">", nil
	}}
	_, plan := newPlanFixture(upper, wrap)

	out, err := plan.Execute(context.Background(), map[string]string{
		"plan": `{"steps":[
			{"id":"u","tool":"upper","params":{"text":"hi"}},
			{"id":"w","tool":"wrap","params":{"text":"${steps.u}"}}
		]}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"final": "<HI>"`) {
		t.Fatalf("expected final=<HI>, got: %s", out)
	}
}

func TestToolPlanVarsSubstitution(t *testing.T) {
	echo := &stubTool{name: "echo", exec: func(_ context.Context, in map[string]string) (string, error) {
		return in["text"], nil
	}}
	_, plan := newPlanFixture(echo)

	out, err := plan.Execute(context.Background(), map[string]string{
		"plan": `{"steps":[{"id":"s","tool":"echo","params":{"text":"hello ${vars.who}"}}]}`,
		"vars": `{"who":"world"}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"final": "hello world"`) {
		t.Fatalf("expected substitution, got: %s", out)
	}
}

func TestToolPlanInvalidJSON(t *testing.T) {
	_, plan := newPlanFixture()
	_, err := plan.Execute(context.Background(), map[string]string{"plan": "{not json"})
	if err == nil || !strings.Contains(err.Error(), "invalid plan JSON") {
		t.Fatalf("expected invalid JSON error, got: %v", err)
	}
}

func TestToolPlanEmptyPlan(t *testing.T) {
	_, plan := newPlanFixture()
	_, err := plan.Execute(context.Background(), map[string]string{"plan": `{"steps":[]}`})
	if err == nil || !strings.Contains(err.Error(), "no steps") {
		t.Fatalf("expected no-steps error, got: %v", err)
	}
}

func TestToolPlanMissingPlanInput(t *testing.T) {
	_, plan := newPlanFixture()
	_, err := plan.Execute(context.Background(), map[string]string{})
	if err == nil || !strings.Contains(err.Error(), "plan is required") {
		t.Fatalf("expected plan-required error, got: %v", err)
	}
}

func TestToolPlanTooManySteps(t *testing.T) {
	echo := &stubTool{name: "echo", exec: func(_ context.Context, _ map[string]string) (string, error) { return "ok", nil }}
	_, plan := newPlanFixture(echo)
	// Build a 21-step plan
	var b strings.Builder
	b.WriteString(`{"steps":[`)
	for i := 0; i < 21; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"id":"s`)
		b.WriteString(string(rune('a' + i%26)))
		b.WriteString(`_`)
		b.WriteString(string(rune('0' + i/26)))
		b.WriteString(`","tool":"echo","params":{}}`)
	}
	b.WriteString(`]}`)
	_, err := plan.Execute(context.Background(), map[string]string{"plan": b.String()})
	if err == nil || !strings.Contains(err.Error(), "max 20") {
		t.Fatalf("expected too-many-steps error, got: %v", err)
	}
}

func TestToolPlanDuplicateStepID(t *testing.T) {
	echo := &stubTool{name: "echo", exec: func(_ context.Context, _ map[string]string) (string, error) { return "ok", nil }}
	_, plan := newPlanFixture(echo)
	_, err := plan.Execute(context.Background(), map[string]string{
		"plan": `{"steps":[{"id":"s","tool":"echo","params":{}},{"id":"s","tool":"echo","params":{}}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate step id") {
		t.Fatalf("expected duplicate-id error, got: %v", err)
	}
}

func TestToolPlanMissingTool(t *testing.T) {
	_, plan := newPlanFixture()
	_, err := plan.Execute(context.Background(), map[string]string{
		"plan": `{"steps":[{"id":"s","tool":"","params":{}}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "missing tool") {
		t.Fatalf("expected missing-tool error, got: %v", err)
	}
}

func TestToolPlanRecursiveBan(t *testing.T) {
	_, plan := newPlanFixture()
	_, err := plan.Execute(context.Background(), map[string]string{
		"plan": `{"steps":[{"id":"s","tool":"tool_plan","params":{}}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot recursively") {
		t.Fatalf("expected recursion-ban error, got: %v", err)
	}
}

func TestToolPlanUnknownStepRef(t *testing.T) {
	echo := &stubTool{name: "echo", exec: func(_ context.Context, in map[string]string) (string, error) { return in["text"], nil }}
	_, plan := newPlanFixture(echo)
	_, err := plan.Execute(context.Background(), map[string]string{
		"plan": `{"steps":[{"id":"s","tool":"echo","params":{"text":"${steps.never}"}}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "unknown step reference") {
		t.Fatalf("expected unknown-step error, got: %v", err)
	}
}

func TestToolPlanUnknownVarsRef(t *testing.T) {
	echo := &stubTool{name: "echo", exec: func(_ context.Context, in map[string]string) (string, error) { return in["text"], nil }}
	_, plan := newPlanFixture(echo)
	_, err := plan.Execute(context.Background(), map[string]string{
		"plan": `{"steps":[{"id":"s","tool":"echo","params":{"text":"${vars.nope}"}}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "unknown vars reference") {
		t.Fatalf("expected unknown-vars error, got: %v", err)
	}
}

func TestToolPlanReturnsPartialOnStepFailure(t *testing.T) {
	ok := &stubTool{name: "ok", exec: func(_ context.Context, _ map[string]string) (string, error) { return "first-output", nil }}
	boom := &stubTool{name: "boom", exec: func(_ context.Context, _ map[string]string) (string, error) {
		return "", errors.New("kaboom")
	}}
	_, plan := newPlanFixture(ok, boom)
	partial, err := plan.Execute(context.Background(), map[string]string{
		"plan": `{"steps":[{"id":"a","tool":"ok","params":{}},{"id":"b","tool":"boom","params":{}}]}`,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `step "b"`) {
		t.Fatalf("error should name failing step, got: %v", err)
	}
	if !strings.Contains(partial, `"a": "first-output"`) {
		t.Fatalf("partial should contain step-a output, got: %s", partial)
	}
}

// --- Policy integration tests ----------------------------------------------

// startPolicyBus spins up an IPC bus with a fake policy handler that returns
// `decision` for every check.
func startPolicyBus(t *testing.T, decision policy.Decision) *ipc.Bus {
	t.Helper()
	bus := ipc.NewBus()
	bus.Handle("policy", "policy.check", func(msg ipc.Message) (ipc.Message, error) {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: policy.CheckResponse{Decision: decision}}, nil
	})
	t.Cleanup(func() { bus.Close() })
	return bus
}

func TestToolPlanPolicyAllowsAll(t *testing.T) {
	echo := &stubTool{name: "echo", exec: func(_ context.Context, in map[string]string) (string, error) { return in["text"], nil }}
	reg := agent.NewToolRegistry()
	reg.Register(echo)
	bus := startPolicyBus(t, policy.Allow)
	plan := NewToolPlanTool(reg, bus)
	reg.Register(plan)

	ctx := agent.WithToolCaller(context.Background(), "t1", "a1", "u1")
	out, err := plan.Execute(ctx, map[string]string{
		"plan": `{"steps":[{"id":"s","tool":"echo","params":{"text":"hi"}}]}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"final": "hi"`) {
		t.Fatalf("expected final=hi, got: %s", out)
	}
}

func TestToolPlanPolicyDenies(t *testing.T) {
	echo := &stubTool{name: "echo", exec: func(_ context.Context, _ map[string]string) (string, error) { return "x", nil }}
	reg := agent.NewToolRegistry()
	reg.Register(echo)
	bus := startPolicyBus(t, policy.Deny)
	plan := NewToolPlanTool(reg, bus)
	reg.Register(plan)

	ctx := agent.WithToolCaller(context.Background(), "t1", "a1", "u1")
	_, err := plan.Execute(ctx, map[string]string{
		"plan": `{"steps":[{"id":"s","tool":"echo","params":{}}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "policy denied") {
		t.Fatalf("expected policy-denied error, got: %v", err)
	}
}

func TestToolPlanPolicyRequireApprovalFailsFast(t *testing.T) {
	echo := &stubTool{name: "echo", exec: func(_ context.Context, _ map[string]string) (string, error) { return "x", nil }}
	reg := agent.NewToolRegistry()
	reg.Register(echo)
	bus := startPolicyBus(t, policy.RequireApproval)
	plan := NewToolPlanTool(reg, bus)
	reg.Register(plan)

	ctx := agent.WithToolCaller(context.Background(), "t1", "a1", "u1")
	_, err := plan.Execute(ctx, map[string]string{
		"plan": `{"steps":[{"id":"s","tool":"echo","params":{}}]}`,
	})
	if err == nil || !strings.Contains(err.Error(), "requires approval") {
		t.Fatalf("expected approval-required error, got: %v", err)
	}
}

func TestToolPlanNoBusSkipsPolicy(t *testing.T) {
	echo := &stubTool{name: "echo", exec: func(_ context.Context, in map[string]string) (string, error) { return in["text"], nil }}
	reg := agent.NewToolRegistry()
	reg.Register(echo)
	// No bus — policy checks are skipped (matches runtime behavior when the
	// policy module isn't registered).
	plan := NewToolPlanTool(reg, nil)

	out, err := plan.Execute(context.Background(), map[string]string{
		"plan": `{"steps":[{"id":"s","tool":"echo","params":{"text":"hi"}}]}`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"final": "hi"`) {
		t.Fatalf("expected final=hi, got: %s", out)
	}
}
