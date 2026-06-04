package tools

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func TestOrchestrateToolName(t *testing.T) {
	tool := NewOrchestrateTool(nil)
	if tool.Name() != "orchestrate_agents" {
		t.Fatal("wrong name")
	}
}

func TestOrchestrateToolParams(t *testing.T) {
	tool := NewOrchestrateTool(nil)
	params := tool.Parameters()
	if _, ok := params["tasks"]; !ok {
		t.Fatal("missing tasks param")
	}
}

// capturedChat records what each child agent.chat request looked like.
type capturedChat struct {
	mu   sync.Mutex
	reqs []agent.ChatRequest
	trc  []string
}

func (c *capturedChat) add(r agent.ChatRequest, trace string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reqs = append(c.reqs, r)
	c.trc = append(c.trc, trace)
}

// orchestrateBus wires a fake agent_runtime that echoes, recording each child
// request. Agents whose name starts with "bad" return an error.
func orchestrateBus(t *testing.T, cap *capturedChat) *ipc.Bus {
	t.Helper()
	bus := ipc.NewBus()
	t.Cleanup(bus.Close)
	bus.Handle("agent_runtime", "agent.chat", func(msg ipc.Message) (ipc.Message, error) {
		req := msg.Payload.(agent.ChatRequest)
		cap.add(req, msg.TraceID)
		if strings.HasPrefix(req.Agent, "bad") {
			return ipc.Message{}, context.DeadlineExceeded
		}
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: agent.ChatResponse{Agent: req.Agent, Content: "done:" + req.Agent}}, nil
	})
	return bus
}

func callerCtx(tenant, user string) context.Context {
	return agent.WithToolCaller(context.Background(), tenant, "parent", user)
}

func TestOrchestrateRunsConcurrentlyAndCollects(t *testing.T) {
	cap := &capturedChat{}
	tool := NewOrchestrateTool(orchestrateBus(t, cap))
	out, err := tool.Execute(callerCtx("acme", "jane"),
		map[string]string{"tasks": `[{"agent":"a1","message":"x"},{"agent":"a2","message":"y"},{"agent":"a3","message":"z"}]`})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, a := range []string{"a1", "a2", "a3"} {
		if !strings.Contains(out, "done:"+a) {
			t.Fatalf("missing result for %s: %s", a, out)
		}
	}
	if len(cap.reqs) != 3 {
		t.Fatalf("expected 3 child calls, got %d", len(cap.reqs))
	}
}

func TestOrchestrateForcesCallerTenantAndUser(t *testing.T) {
	cap := &capturedChat{}
	tool := NewOrchestrateTool(orchestrateBus(t, cap))
	// The model tries to target another tenant; it must be ignored.
	_, err := tool.Execute(callerCtx("acme", "jane"),
		map[string]string{"tasks": `[{"tenant":"victim","agent":"a1","message":"x"},{"tenant":"victim","agent":"a2","message":"y"}]`})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, r := range cap.reqs {
		if r.Tenant != "acme" {
			t.Fatalf("child ran in tenant %q, must be forced to caller's 'acme'", r.Tenant)
		}
		if r.User != "jane" {
			t.Fatalf("child user = %q, want inherited 'jane'", r.User)
		}
	}
}

func TestOrchestrateOneFailureDoesNotKillBatch(t *testing.T) {
	cap := &capturedChat{}
	tool := NewOrchestrateTool(orchestrateBus(t, cap))
	out, err := tool.Execute(callerCtx("acme", "jane"),
		map[string]string{"tasks": `[{"agent":"good","message":"x"},{"agent":"bad1","message":"y"}]`})
	if err != nil {
		t.Fatalf("batch should not fail when one child errors: %v", err)
	}
	if !strings.Contains(out, "done:good") {
		t.Fatalf("good child result missing: %s", out)
	}
	if !strings.Contains(out, "Error:") {
		t.Fatalf("failed child should surface an error: %s", out)
	}
}

func TestOrchestrateFanoutCap(t *testing.T) {
	cap := &capturedChat{}
	tool := NewOrchestrateTool(orchestrateBus(t, cap))
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < 11; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"agent":"a","message":"m"}`)
	}
	b.WriteString("]")
	if _, err := tool.Execute(callerCtx("acme", "jane"), map[string]string{"tasks": b.String()}); err == nil {
		t.Fatal("exceeding the fanout cap should error")
	}
}

func TestOrchestrateRequiresTenantContext(t *testing.T) {
	cap := &capturedChat{}
	tool := NewOrchestrateTool(orchestrateBus(t, cap))
	// No ToolCaller in context.
	if _, err := tool.Execute(context.Background(), map[string]string{"tasks": `[{"agent":"a","message":"m"}]`}); err == nil {
		t.Fatal("missing tenant context should error")
	}
}

func TestOrchestrateChildrenShareTrace(t *testing.T) {
	cap := &capturedChat{}
	tool := NewOrchestrateTool(orchestrateBus(t, cap))
	tool.Execute(callerCtx("acme", "jane"),
		map[string]string{"tasks": `[{"agent":"a1","message":"x"},{"agent":"a2","message":"y"}]`})
	if len(cap.trc) != 2 {
		t.Fatalf("expected 2 traces, got %d", len(cap.trc))
	}
	if cap.trc[0] == "" || cap.trc[0] != cap.trc[1] {
		t.Fatalf("children should share one non-empty trace id: %v", cap.trc)
	}
}
