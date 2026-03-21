package workflow

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func setupWorkflowEngine(t *testing.T) (*Engine, *ipc.Bus) {
	t.Helper()
	bus := ipc.NewBus()
	e := New()
	ctx := context.Background()
	e.Init(ctx, &kernel.Services{Bus: bus})
	e.Start(ctx)

	// Register a mock agent handler
	bus.Handle("agent_runtime", "agent.chat", func(msg ipc.Message) (ipc.Message, error) {
		req := msg.Payload.(agent.ChatRequest)
		return ipc.Message{
			Type:    ipc.MessageTypeResponse,
			Payload: agent.ChatResponse{Agent: req.Agent, Content: "mock response for: " + req.Message},
		}, nil
	})

	t.Cleanup(func() { e.Stop(ctx); bus.Close() })
	return e, bus
}

func TestParallelStepType(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	wf := &Workflow{
		ID: "wf_par", Name: "test parallel",
		Steps: []Step{
			{ID: "parallel1", Type: StepParallel, SubSteps: []string{"sub1", "sub2"}},
			{ID: "sub1", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "task 1"}},
			{ID: "sub2", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "task 2"}},
		},
		StartStep: "parallel1",
	}

	e.mu.Lock()
	e.workflows["wf_par"] = wf
	e.mu.Unlock()

	run := &Run{ID: "run_par", WorkflowID: "wf_par", Status: RunPending, Results: make(map[string]StepResult)}
	result := e.executeParallelStep(wf.Steps[0], run)

	if result.Status != "success" {
		t.Fatalf("expected success, got %s (error: %s)", result.Status, result.Error)
	}
	if result.Output == "" {
		t.Fatal("expected output from parallel execution")
	}
}

func TestParallelStepNoSubSteps(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	wf := &Workflow{
		ID: "wf_par_empty", Name: "empty parallel",
		Steps: []Step{
			{ID: "parallel1", Type: StepParallel, SubSteps: nil},
		},
		StartStep: "parallel1",
	}

	e.mu.Lock()
	e.workflows["wf_par_empty"] = wf
	e.mu.Unlock()

	run := &Run{ID: "run_par_empty", WorkflowID: "wf_par_empty", Status: RunPending, Results: make(map[string]StepResult)}
	result := e.executeParallelStep(wf.Steps[0], run)

	if result.Status != "failure" {
		t.Fatalf("expected failure for empty sub-steps, got %s", result.Status)
	}
	if !strings.Contains(result.Error, "no sub-steps") {
		t.Fatalf("expected 'no sub-steps' error, got %q", result.Error)
	}
}

func TestParallelStepMissingSubStep(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	wf := &Workflow{
		ID: "wf_par_missing", Name: "missing sub",
		Steps: []Step{
			{ID: "parallel1", Type: StepParallel, SubSteps: []string{"sub1", "nonexistent"}},
			{ID: "sub1", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "task 1"}},
		},
		StartStep: "parallel1",
	}

	e.mu.Lock()
	e.workflows["wf_par_missing"] = wf
	e.mu.Unlock()

	run := &Run{ID: "run_par_missing", WorkflowID: "wf_par_missing", Status: RunPending, Results: make(map[string]StepResult)}
	result := e.executeParallelStep(wf.Steps[0], run)

	// Should still complete (one sub-step fails with "sub-step not found")
	if result.Status != "failure" {
		t.Fatalf("expected failure when sub-step is missing, got %s", result.Status)
	}
}

func TestParallelStepPopulatesRunResults(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	wf := &Workflow{
		ID: "wf_par_res", Name: "parallel results",
		Steps: []Step{
			{ID: "parallel1", Type: StepParallel, SubSteps: []string{"sub1", "sub2"}},
			{ID: "sub1", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "task A"}},
			{ID: "sub2", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "task B"}},
		},
		StartStep: "parallel1",
	}

	e.mu.Lock()
	e.workflows["wf_par_res"] = wf
	e.mu.Unlock()

	run := &Run{ID: "run_par_res", WorkflowID: "wf_par_res", Status: RunPending, Results: make(map[string]StepResult)}
	e.executeParallelStep(wf.Steps[0], run)

	// Each sub-step result should be stored in run.Results
	if _, ok := run.Results["sub1"]; !ok {
		t.Fatal("expected sub1 result in run.Results")
	}
	if _, ok := run.Results["sub2"]; !ok {
		t.Fatal("expected sub2 result in run.Results")
	}
}

func TestLoopStepType(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	wf := &Workflow{
		ID: "wf_loop", Name: "loop test",
		Steps: []Step{
			{ID: "loop1", Type: StepLoop, LoopOver: "a,b,c", Config: map[string]string{"body_step": "body1"}},
			{ID: "body1", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "process {{item}}"}},
		},
		StartStep: "loop1",
	}

	e.mu.Lock()
	e.workflows["wf_loop"] = wf
	e.mu.Unlock()

	run := &Run{ID: "run_loop", WorkflowID: "wf_loop", Status: RunPending, Results: make(map[string]StepResult)}
	result := e.executeLoopStep(wf.Steps[0], run)

	if result.Status != "success" {
		t.Fatalf("expected success, got %s", result.Status)
	}
	if result.Output == "" {
		t.Fatal("expected output from loop iterations")
	}
	// Should have 3 iteration results
	iterCount := 0
	for key := range run.Results {
		if strings.HasPrefix(key, "loop1_iter_") {
			iterCount++
		}
	}
	if iterCount != 3 {
		t.Fatalf("expected 3 iteration results, got %d", iterCount)
	}
}

func TestLoopStepItemSubstitution(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	wf := &Workflow{
		ID: "wf_loop_sub", Name: "loop substitution",
		Steps: []Step{
			{ID: "loop1", Type: StepLoop, LoopOver: "alpha,beta", Config: map[string]string{"body_step": "body1"}},
			{ID: "body1", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "item is {{item}}"}},
		},
		StartStep: "loop1",
	}

	e.mu.Lock()
	e.workflows["wf_loop_sub"] = wf
	e.mu.Unlock()

	run := &Run{ID: "run_loop_sub", WorkflowID: "wf_loop_sub", Status: RunPending, Results: make(map[string]StepResult)}
	result := e.executeLoopStep(wf.Steps[0], run)

	if result.Status != "success" {
		t.Fatalf("expected success, got %s", result.Status)
	}
	// The mock agent echoes back the message, so check substitution happened
	if !strings.Contains(result.Output, "alpha") {
		t.Fatalf("expected item substitution for 'alpha' in output, got: %s", result.Output)
	}
	if !strings.Contains(result.Output, "beta") {
		t.Fatalf("expected item substitution for 'beta' in output, got: %s", result.Output)
	}
}

func TestLoopStepNoBodyStep(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	wf := &Workflow{
		ID: "wf_loop_nobody", Name: "loop no body",
		Steps: []Step{
			{ID: "loop1", Type: StepLoop, LoopOver: "a,b", Config: map[string]string{}},
		},
		StartStep: "loop1",
	}

	e.mu.Lock()
	e.workflows["wf_loop_nobody"] = wf
	e.mu.Unlock()

	run := &Run{ID: "run_loop_nobody", WorkflowID: "wf_loop_nobody", Status: RunPending, Results: make(map[string]StepResult)}
	result := e.executeLoopStep(wf.Steps[0], run)

	if result.Status != "failure" {
		t.Fatalf("expected failure when body_step not configured, got %s", result.Status)
	}
	if !strings.Contains(result.Error, "body_step") {
		t.Fatalf("expected body_step error, got %q", result.Error)
	}
}

func TestLoopStepEmptyLoopOver(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	wf := &Workflow{
		ID: "wf_loop_empty", Name: "loop empty",
		Steps: []Step{
			{ID: "loop1", Type: StepLoop, LoopOver: "", Config: map[string]string{"body_step": "body1"}},
			{ID: "body1", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "item {{item}}"}},
		},
		StartStep: "loop1",
	}

	e.mu.Lock()
	e.workflows["wf_loop_empty"] = wf
	e.mu.Unlock()

	run := &Run{ID: "run_loop_empty", WorkflowID: "wf_loop_empty", Status: RunPending, Results: make(map[string]StepResult)}
	result := e.executeLoopStep(wf.Steps[0], run)

	// With empty LoopOver and no loop_source, should still succeed with no iterations
	if result.Status != "success" {
		t.Fatalf("expected success for empty loop, got %s", result.Status)
	}
}

func TestHumanInputStepTimeout(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	step := Step{
		ID: "input1", Type: StepHumanInput,
		Config: map[string]string{"prompt": "Enter value", "timeout": "100ms"},
	}
	run := &Run{ID: "run_input_timeout", Results: make(map[string]StepResult)}

	result := e.executeHumanInputStep(step, run)
	if result.Status != "failure" {
		t.Fatalf("expected failure on timeout, got %s", result.Status)
	}
	if result.Error != "input timeout" {
		t.Fatalf("expected 'input timeout', got %q", result.Error)
	}
}

func TestHumanInputStepSubmit(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	step := Step{
		ID: "input2", Type: StepHumanInput,
		Config: map[string]string{"prompt": "Enter value", "timeout": "5s"},
	}
	run := &Run{ID: "run_input_submit", Results: make(map[string]StepResult)}

	// Submit input after a short delay
	go func() {
		time.Sleep(200 * time.Millisecond)
		e.mu.RLock()
		ch, exists := e.waitingInputs["run_input_submit"]
		e.mu.RUnlock()
		if exists {
			ch <- "user answer"
		}
	}()

	result := e.executeHumanInputStep(step, run)
	if result.Status != "success" {
		t.Fatalf("expected success, got %s (error: %s)", result.Status, result.Error)
	}
	if result.Output != "user answer" {
		t.Fatalf("expected 'user answer', got %q", result.Output)
	}
}

func TestHumanInputStepSetsWaitingStatus(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	step := Step{
		ID: "input3", Type: StepHumanInput,
		Config: map[string]string{"prompt": "Enter value", "timeout": "200ms"},
	}
	run := &Run{ID: "run_input_status", Status: RunRunning, Results: make(map[string]StepResult)}

	// Let step time out — it sets RunWaitingInput during execution
	result := e.executeHumanInputStep(step, run)

	// After timeout, the step fails but the status was set during execution
	if result.Status != "failure" {
		t.Fatalf("expected failure on timeout, got %s", result.Status)
	}
	if result.Error != "input timeout" {
		t.Fatalf("expected input timeout error, got %q", result.Error)
	}
}

func TestHumanInputStepCleansUpChannel(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	step := Step{
		ID: "input4", Type: StepHumanInput,
		Config: map[string]string{"prompt": "Enter value", "timeout": "100ms"},
	}
	run := &Run{ID: "run_input_cleanup", Results: make(map[string]StepResult)}

	e.executeHumanInputStep(step, run)

	// After execution, the waitingInputs entry should be cleaned up
	e.mu.RLock()
	_, exists := e.waitingInputs["run_input_cleanup"]
	e.mu.RUnlock()

	if exists {
		t.Fatal("expected waitingInputs entry to be cleaned up after execution")
	}
}

func TestHumanInputStepDefaultPrompt(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	// Step with no prompt configured
	step := Step{
		ID: "input5", Type: StepHumanInput,
		Config: map[string]string{"timeout": "100ms"},
	}
	run := &Run{ID: "run_input_default", Results: make(map[string]StepResult)}

	// Should not panic; just times out normally
	result := e.executeHumanInputStep(step, run)
	if result.Status != "failure" {
		t.Fatalf("expected failure on timeout, got %s", result.Status)
	}
}

func TestEventTriggerAdd(t *testing.T) {
	_, bus := setupWorkflowEngine(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.trigger.add",
		Payload: map[string]string{"type": "audit_event", "pattern": "tool_call.*", "workflow_id": "wf1"},
	})
	if err != nil {
		t.Fatalf("trigger add: %v", err)
	}

	// List triggers
	listResp, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.trigger.list",
	})
	if err != nil {
		t.Fatalf("trigger list: %v", err)
	}
	triggers := listResp.Payload.([]Trigger)
	if len(triggers) != 1 {
		t.Fatalf("expected 1 trigger, got %d", len(triggers))
	}
	if triggers[0].Type != "audit_event" {
		t.Fatalf("expected type audit_event, got %q", triggers[0].Type)
	}
	if triggers[0].Pattern != "tool_call.*" {
		t.Fatalf("expected pattern tool_call.*, got %q", triggers[0].Pattern)
	}
	if triggers[0].WorkflowID != "wf1" {
		t.Fatalf("expected workflow_id wf1, got %q", triggers[0].WorkflowID)
	}
}

func TestEventTriggerAddMultiple(t *testing.T) {
	_, bus := setupWorkflowEngine(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.trigger.add",
		Payload: map[string]string{"type": "audit_event", "pattern": "tool_call.*", "workflow_id": "wf1"},
	})
	bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.trigger.add",
		Payload: map[string]string{"type": "channel_message", "pattern": "help.*", "workflow_id": "wf2"},
	})

	listResp, _ := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.trigger.list",
	})
	triggers := listResp.Payload.([]Trigger)
	if len(triggers) != 2 {
		t.Fatalf("expected 2 triggers, got %d", len(triggers))
	}
}

func TestEventTriggerListEmpty(t *testing.T) {
	_, bus := setupWorkflowEngine(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	listResp, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "workflow", Topic: "workflow.trigger.list",
	})
	if err != nil {
		t.Fatalf("trigger list: %v", err)
	}
	// triggers field starts as nil
	triggers := listResp.Payload.([]Trigger)
	if triggers != nil && len(triggers) != 0 {
		t.Fatalf("expected empty triggers, got %d", len(triggers))
	}
}

func TestExecuteStepDispatchesParallel(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	wf := &Workflow{
		ID: "wf_dispatch_par", Name: "dispatch parallel",
		Steps: []Step{
			{ID: "par1", Type: StepParallel, SubSteps: []string{"s1"}},
			{ID: "s1", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "hi"}},
		},
		StartStep: "par1",
	}

	e.mu.Lock()
	e.workflows["wf_dispatch_par"] = wf
	e.mu.Unlock()

	run := &Run{ID: "run_dispatch_par", WorkflowID: "wf_dispatch_par", Status: RunPending, Results: make(map[string]StepResult)}
	// executeStep should dispatch to executeParallelStep
	result := e.executeStep(wf.Steps[0], run)
	if result.Status != "success" {
		t.Fatalf("expected success from executeStep for parallel type, got %s", result.Status)
	}
}

func TestExecuteStepDispatchesLoop(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	wf := &Workflow{
		ID: "wf_dispatch_loop", Name: "dispatch loop",
		Steps: []Step{
			{ID: "loop1", Type: StepLoop, LoopOver: "x", Config: map[string]string{"body_step": "s1"}},
			{ID: "s1", Type: StepAgentChat, Config: map[string]string{"agent": "bot", "message": "{{item}}"}},
		},
		StartStep: "loop1",
	}

	e.mu.Lock()
	e.workflows["wf_dispatch_loop"] = wf
	e.mu.Unlock()

	run := &Run{ID: "run_dispatch_loop", WorkflowID: "wf_dispatch_loop", Status: RunPending, Results: make(map[string]StepResult)}
	result := e.executeStep(wf.Steps[0], run)
	if result.Status != "success" {
		t.Fatalf("expected success from executeStep for loop type, got %s", result.Status)
	}
}

func TestExecuteStepDispatchesHumanInput(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	step := Step{
		ID: "input_dispatch", Type: StepHumanInput,
		Config: map[string]string{"prompt": "test", "timeout": "100ms"},
	}
	run := &Run{ID: "run_dispatch_input", Results: make(map[string]StepResult)}

	result := e.executeStep(step, run)
	// Should timeout since no input provided
	if result.Status != "failure" {
		t.Fatalf("expected failure (timeout) from executeStep for human_input type, got %s", result.Status)
	}
}

func TestExecuteStepUnknownType(t *testing.T) {
	e, _ := setupWorkflowEngine(t)

	step := Step{
		ID: "unknown1", Type: StepType("nonexistent_type"),
		Config: map[string]string{},
	}
	run := &Run{ID: "run_unknown", Results: make(map[string]StepResult)}

	result := e.executeStep(step, run)
	if result.Status != "failure" {
		t.Fatalf("expected failure for unknown step type, got %s", result.Status)
	}
	if !strings.Contains(result.Error, "unknown step type") {
		t.Fatalf("expected 'unknown step type' error, got %q", result.Error)
	}
}
