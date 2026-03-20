package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func setupWorkflowTest(t *testing.T) (*Engine, *ipc.Bus) {
	t.Helper()
	bus := ipc.NewBus()

	// Mock agent runtime
	bus.Handle("agent_runtime", "agent.chat", func(msg ipc.Message) (ipc.Message, error) {
		req := msg.Payload.(agent.ChatRequest)
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: agent.ChatResponse{
			Agent: req.Agent, Content: "Agent processed: " + req.Message,
		}}, nil
	})

	e := New()
	ctx := context.Background()
	e.Init(ctx, &kernel.Services{Bus: bus})
	e.Start(ctx)

	t.Cleanup(func() { e.Stop(ctx); bus.Close() })
	return e, bus
}

func TestWorkflowEngineImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Engine)(nil)
}

func TestWorkflowRegisterAndList(t *testing.T) {
	_, bus := setupWorkflowTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.register",
		Payload: Workflow{Name: "test-wf", Tenant: "demo", StartStep: "s1",
			Steps: []Step{{ID: "s1", Name: "greet", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "Hello"}}}},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	wfID := resp.Payload.(string)
	if wfID == "" {
		t.Fatal("expected workflow ID")
	}

	resp, _ = bus.Request(ctx, ipc.Message{Source: "test", Target: "workflow", Topic: "workflow.list"})
	ids := resp.Payload.([]string)
	if len(ids) != 1 {
		t.Fatalf("expected 1, got %d", len(ids))
	}
}

func TestWorkflowRunSimple(t *testing.T) {
	_, bus := setupWorkflowTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Register a simple 2-step workflow
	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.register",
		Payload: Workflow{ID: "wf1", Name: "greet-flow", Tenant: "demo", StartStep: "step1",
			Steps: []Step{
				{ID: "step1", Name: "Ask agent", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "Say hello"}, OnSuccess: "step2"},
				{ID: "step2", Name: "Confirm", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "Previous result: {{step1.output}}"}},
			}},
	})

	// Run it
	resp, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.run",
		Payload: map[string]string{"workflow_id": "wf1"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	runID := resp.Payload.(string)

	// Wait for completion
	time.Sleep(2 * time.Second)

	resp, _ = bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.status", Payload: runID,
	})
	run := resp.Payload.(Run)
	if run.Status != RunCompleted {
		t.Fatalf("expected completed, got %s (error: %s)", run.Status, run.Error)
	}
	if len(run.Results) != 2 {
		t.Fatalf("expected 2 step results, got %d", len(run.Results))
	}
}

func TestWorkflowRunWithWebhook(t *testing.T) {
	_, bus := setupWorkflowTest(t)

	webhookReceived := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		webhookReceived <- body["event"]
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.register",
		Payload: Workflow{ID: "wf2", Name: "webhook-flow", Tenant: "demo", StartStep: "s1",
			Steps: []Step{
				{ID: "s1", Name: "Fire webhook", Type: StepWebhook, Config: map[string]string{
					"url": server.URL, "body": `{"event":"workflow_completed"}`,
				}},
			}},
	})

	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.run",
		Payload: map[string]string{"workflow_id": "wf2"},
	})

	select {
	case event := <-webhookReceived:
		if event != "workflow_completed" {
			t.Fatalf("got %q", event)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("webhook not received")
	}
}

func TestWorkflowRunWithDelay(t *testing.T) {
	_, bus := setupWorkflowTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.register",
		Payload: Workflow{ID: "wf3", Name: "delay-flow", Tenant: "demo", StartStep: "s1",
			Steps: []Step{
				{ID: "s1", Name: "Wait", Type: StepDelay, Config: map[string]string{"duration": "100ms"}, OnSuccess: "s2"},
				{ID: "s2", Name: "Done", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "Done waiting"}},
			}},
	})

	resp, _ := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.run",
		Payload: map[string]string{"workflow_id": "wf3"},
	})
	runID := resp.Payload.(string)

	time.Sleep(2 * time.Second)

	resp, _ = bus.Request(ctx, ipc.Message{Source: "test", Target: "workflow", Topic: "workflow.status", Payload: runID})
	run := resp.Payload.(Run)
	if run.Status != RunCompleted {
		t.Fatalf("expected completed, got %s", run.Status)
	}
}

func TestWorkflowRunNotFound(t *testing.T) {
	_, bus := setupWorkflowTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.run",
		Payload: map[string]string{"workflow_id": "nonexistent"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkflowStatusNotFound(t *testing.T) {
	_, bus := setupWorkflowTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := bus.Request(ctx, ipc.Message{Source: "test", Target: "workflow", Topic: "workflow.status", Payload: "nonexistent"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkflowHealthy(t *testing.T) {
	e, _ := setupWorkflowTest(t)
	h := e.Health(context.Background())
	if !h.Healthy {
		t.Fatalf("expected healthy: %s", h.Message)
	}
}

func TestWorkflowConditionStep(t *testing.T) {
	_, bus := setupWorkflowTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.register",
		Payload: Workflow{ID: "wf4", Name: "condition-flow", Tenant: "demo", StartStep: "s1",
			Steps: []Step{
				{ID: "s1", Name: "Do something", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "check"}, OnSuccess: "s2"},
				{ID: "s2", Name: "Check result", Type: StepCondition, Config: map[string]string{"check_step": "s1"}, OnSuccess: "s3", OnFailure: "s4"},
				{ID: "s3", Name: "Success path", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "success"}},
				{ID: "s4", Name: "Failure path", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "failure"}},
			}},
	})

	resp, _ := bus.Request(ctx, ipc.Message{Source: "test", Target: "workflow", Topic: "workflow.run", Payload: map[string]string{"workflow_id": "wf4"}})
	runID := resp.Payload.(string)

	time.Sleep(3 * time.Second)

	resp, _ = bus.Request(ctx, ipc.Message{Source: "test", Target: "workflow", Topic: "workflow.status", Payload: runID})
	run := resp.Payload.(Run)
	if run.Status != RunCompleted {
		t.Fatalf("expected completed, got %s (error: %s)", run.Status, run.Error)
	}
	// Should have taken success path (s1 -> s2 -> s3), not failure (s4)
	if _, ok := run.Results["s3"]; !ok {
		t.Fatal("expected s3 (success path) to have been executed")
	}
}
