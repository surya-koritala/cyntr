package workflow

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

var logger = log.Default().WithModule("workflow")

// Engine is the workflow execution kernel module.
type Engine struct {
	mu            sync.RWMutex
	bus           *ipc.Bus
	workflows     map[string]*Workflow    // workflow ID -> definition
	runs          map[string]*Run         // run ID -> execution state
	waitingInputs map[string]chan string   // run_id -> input channel
	triggers      []Trigger
	counter       int64
}

func New() *Engine {
	return &Engine{
		workflows:     make(map[string]*Workflow),
		runs:          make(map[string]*Run),
		waitingInputs: make(map[string]chan string),
	}
}

func (e *Engine) Name() string           { return "workflow" }
func (e *Engine) Dependencies() []string { return []string{"agent_runtime"} }

func (e *Engine) Init(ctx context.Context, svc *kernel.Services) error {
	e.bus = svc.Bus
	return nil
}

func (e *Engine) Start(ctx context.Context) error {
	e.bus.Handle("workflow", "workflow.register", e.handleRegister)
	e.bus.Handle("workflow", "workflow.run", e.handleRun)
	e.bus.Handle("workflow", "workflow.status", e.handleStatus)
	e.bus.Handle("workflow", "workflow.list", e.handleList)
	e.bus.Handle("workflow", "workflow.list_runs", e.handleListRuns)
	e.bus.Handle("workflow", "workflow.get", e.handleGet)
	e.bus.Handle("workflow", "workflow.submit_input", e.handleSubmitInput)
	e.bus.Handle("workflow", "workflow.trigger.add", e.handleTriggerAdd)
	e.bus.Handle("workflow", "workflow.trigger.list", e.handleTriggerList)

	// Subscribe to activity events to match triggers
	e.bus.Subscribe("workflow", "agent.activity", e.handleActivityEvent)

	return nil
}

func (e *Engine) handleActivityEvent(msg ipc.Message) (ipc.Message, error) {
	e.mu.RLock()
	triggers := make([]Trigger, len(e.triggers))
	copy(triggers, e.triggers)
	e.mu.RUnlock()

	if len(triggers) == 0 {
		return ipc.Message{}, nil
	}

	// Extract event detail for matching
	var eventType, eventDetail string
	if evt, ok := msg.Payload.(agent.ActivityEvent); ok {
		eventType = evt.Type
		eventDetail = evt.Detail
	} else {
		return ipc.Message{}, nil
	}

	for _, trigger := range triggers {
		if trigger.Type != "audit_event" {
			continue
		}
		// Match pattern against event type or detail
		matched, _ := regexp.MatchString(trigger.Pattern, eventType)
		if !matched {
			matched, _ = regexp.MatchString(trigger.Pattern, eventDetail)
		}
		if matched {
			logger.Info("trigger matched, starting workflow", map[string]any{
				"trigger_pattern": trigger.Pattern, "workflow_id": trigger.WorkflowID, "event_type": eventType,
			})
			// Start the workflow
			go func(wfID string) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				e.bus.Request(ctx, ipc.Message{
					Source: "workflow_trigger", Target: "workflow", Topic: "workflow.run",
					Payload: map[string]string{"workflow_id": wfID},
				})
			}(trigger.WorkflowID)
		}
	}
	return ipc.Message{}, nil
}

func (e *Engine) Stop(ctx context.Context) error { return nil }

func (e *Engine) Health(ctx context.Context) kernel.HealthStatus {
	e.mu.RLock()
	wfCount := len(e.workflows)
	runCount := len(e.runs)
	e.mu.RUnlock()
	return kernel.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("%d workflows, %d runs", wfCount, runCount),
	}
}

func (e *Engine) handleRegister(msg ipc.Message) (ipc.Message, error) {
	wf, ok := msg.Payload.(Workflow)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected Workflow, got %T", msg.Payload)
	}

	e.mu.Lock()
	if wf.ID == "" {
		e.counter++
		wf.ID = fmt.Sprintf("wf_%d", e.counter)
	}
	e.workflows[wf.ID] = &wf
	e.mu.Unlock()

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: wf.ID}, nil
}

func (e *Engine) handleRun(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string with workflow_id")
	}

	wfID := req["workflow_id"]
	e.mu.RLock()
	wf, exists := e.workflows[wfID]
	e.mu.RUnlock()
	if !exists {
		return ipc.Message{}, fmt.Errorf("workflow %q not found", wfID)
	}

	e.mu.Lock()
	e.counter++
	runID := fmt.Sprintf("run_%d", e.counter)
	run := &Run{
		ID: runID, WorkflowID: wfID, Tenant: wf.Tenant,
		Status: RunRunning, CurrentStep: wf.StartStep,
		Results: make(map[string]StepResult), StartedAt: time.Now().UTC(),
	}
	e.runs[runID] = run
	e.mu.Unlock()

	// Execute asynchronously
	go e.executeWorkflow(wf, run)

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: runID}, nil
}

func (e *Engine) handleStatus(msg ipc.Message) (ipc.Message, error) {
	runID, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string run ID")
	}

	e.mu.RLock()
	run, exists := e.runs[runID]
	e.mu.RUnlock()
	if !exists {
		return ipc.Message{}, fmt.Errorf("run %q not found", runID)
	}

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: *run}, nil
}

func (e *Engine) handleGet(msg ipc.Message) (ipc.Message, error) {
	id, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string, got %T", msg.Payload)
	}
	e.mu.RLock()
	wf, exists := e.workflows[id]
	e.mu.RUnlock()
	if !exists {
		return ipc.Message{}, fmt.Errorf("workflow %q not found", id)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: wf}, nil
}

func (e *Engine) handleList(msg ipc.Message) (ipc.Message, error) {
	e.mu.RLock()
	var ids []string
	for id := range e.workflows {
		ids = append(ids, id)
	}
	e.mu.RUnlock()
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: ids}, nil
}

func (e *Engine) handleListRuns(msg ipc.Message) (ipc.Message, error) {
	e.mu.RLock()
	var runs []Run
	for _, r := range e.runs {
		runs = append(runs, *r)
	}
	e.mu.RUnlock()
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: runs}, nil
}

func (e *Engine) executeWorkflow(wf *Workflow, run *Run) {
	logger.Info("workflow started", map[string]any{"workflow_id": wf.ID, "run_id": run.ID, "tenant": wf.Tenant})

	stepMap := make(map[string]Step)
	for _, s := range wf.Steps {
		stepMap[s.ID] = s
	}

	currentID := wf.StartStep

	for currentID != "" {
		step, ok := stepMap[currentID]
		if !ok {
			e.failRun(run, fmt.Sprintf("step %q not found", currentID))
			return
		}

		e.mu.Lock()
		run.CurrentStep = currentID
		e.mu.Unlock()

		result := e.executeStep(step, run)

		e.mu.Lock()
		run.Results[step.ID] = result
		e.mu.Unlock()

		if result.Status == "success" {
			currentID = step.OnSuccess
		} else {
			// Retry logic
			retried := false
			for i := 0; i < step.RetryCount; i++ {
				logger.Warn("step retry", map[string]any{"step_id": step.ID, "attempt": i + 1})
				time.Sleep(time.Duration(i+1) * time.Second) // backoff
				retryResult := e.executeStep(step, run)
				e.mu.Lock()
				run.Results[step.ID] = retryResult
				e.mu.Unlock()
				if retryResult.Status == "success" {
					currentID = step.OnSuccess
					retried = true
					break
				}
			}
			if !retried {
				if step.OnFailure != "" {
					currentID = step.OnFailure
				} else {
					e.failRun(run, fmt.Sprintf("step %q failed: %s", step.ID, result.Error))
					return
				}
			}
		}
	}

	e.mu.Lock()
	run.Status = RunCompleted
	run.CompletedAt = time.Now().UTC()
	e.mu.Unlock()
}

func (e *Engine) executeStep(step Step, run *Run) StepResult {
	start := time.Now()
	result := StepResult{StepID: step.ID, Timestamp: start}

	timeout := step.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	switch step.Type {
	case StepToolCall:
		output, err := e.executeToolCall(ctx, step, run)
		result.Output = output
		if err != nil {
			result.Status = "failure"
			result.Error = err.Error()
		} else {
			result.Status = "success"
		}

	case StepAgentChat:
		output, err := e.executeAgentChat(ctx, step, run)
		result.Output = output
		if err != nil {
			result.Status = "failure"
			result.Error = err.Error()
		} else {
			result.Status = "success"
		}

	case StepWebhook:
		output, err := e.executeWebhook(ctx, step)
		result.Output = output
		if err != nil {
			result.Status = "failure"
			result.Error = err.Error()
		} else {
			result.Status = "success"
		}

	case StepDelay:
		dur, _ := time.ParseDuration(step.Config["duration"])
		if dur > 0 {
			time.Sleep(dur)
		}
		result.Status = "success"
		result.Output = fmt.Sprintf("waited %s", dur)

	case StepCondition:
		// Check previous step result
		checkStep := step.Config["check_step"]
		e.mu.RLock()
		prevResult, exists := run.Results[checkStep]
		e.mu.RUnlock()
		if exists && prevResult.Status == "success" {
			result.Status = "success"
		} else {
			result.Status = "failure"
		}

	case StepParallel:
		return e.executeParallelStep(step, run)

	case StepLoop:
		return e.executeLoopStep(step, run)

	case StepHumanInput:
		return e.executeHumanInputStep(step, run)

	default:
		result.Status = "failure"
		result.Error = fmt.Sprintf("unknown step type: %s", step.Type)
	}

	result.Duration = time.Since(start)
	return result
}

func (e *Engine) executeToolCall(ctx context.Context, step Step, run *Run) (string, error) {
	toolName := step.Config["tool"]
	input := make(map[string]string)
	for k, v := range step.Config {
		if k != "tool" {
			input[k] = v
		}
	}

	// Use agent runtime's tool registry via IPC
	// For now, call the tool directly via agent.chat with a tool instruction
	resp, err := e.bus.Request(ctx, ipc.Message{
		Source: "workflow", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent: step.Config["agent"], Tenant: run.Tenant,
			User:    "workflow",
			Message: fmt.Sprintf("Execute tool %s with input: %v", toolName, input),
		},
	})
	if err != nil {
		return "", err
	}

	chatResp, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		return "", fmt.Errorf("unexpected response")
	}
	return chatResp.Content, nil
}

func (e *Engine) executeAgentChat(ctx context.Context, step Step, run *Run) (string, error) {
	agentName := step.Config["agent"]
	message := step.Config["message"]

	// Substitute previous step results into message
	e.mu.RLock()
	for stepID, result := range run.Results {
		message = substituteVar(message, "{{"+stepID+".output}}", result.Output)
	}
	e.mu.RUnlock()

	resp, err := e.bus.Request(ctx, ipc.Message{
		Source: "workflow", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{Agent: agentName, Tenant: run.Tenant, User: "workflow", Message: message},
	})
	if err != nil {
		return "", err
	}
	chatResp, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		return "", fmt.Errorf("unexpected response type: %T", resp.Payload)
	}
	return chatResp.Content, nil
}

func (e *Engine) executeWebhook(ctx context.Context, step Step) (string, error) {
	url := step.Config["url"]
	method := step.Config["method"]
	if method == "" {
		method = "POST"
	}
	body := step.Config["body"]

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewBufferString(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	if auth := step.Config["authorization"]; auth != "" {
		req.Header.Set("Authorization", auth)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var respBody bytes.Buffer
	respBody.ReadFrom(resp.Body)

	if resp.StatusCode >= 400 {
		return respBody.String(), fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return respBody.String(), nil
}

func (e *Engine) executeParallelStep(step Step, run *Run) StepResult {
	start := time.Now()

	if len(step.SubSteps) == 0 {
		return StepResult{StepID: step.ID, Status: "failure", Error: "no sub-steps defined", Duration: time.Since(start), Timestamp: time.Now()}
	}

	// Find sub-step definitions
	stepMap := make(map[string]Step)
	e.mu.RLock()
	wf := e.workflows[run.WorkflowID]
	e.mu.RUnlock()
	if wf != nil {
		for _, s := range wf.Steps {
			stepMap[s.ID] = s
		}
	}

	type subResult struct {
		idx    int
		result StepResult
	}
	results := make([]subResult, len(step.SubSteps))
	var wg sync.WaitGroup

	for i, subID := range step.SubSteps {
		subStep, ok := stepMap[subID]
		if !ok {
			results[i] = subResult{i, StepResult{StepID: subID, Status: "failure", Error: "sub-step not found"}}
			continue
		}
		wg.Add(1)
		go func(idx int, s Step) {
			defer wg.Done()
			result := e.executeStep(s, run)
			results[idx] = subResult{idx, result}
		}(i, subStep)
	}
	wg.Wait()

	// Check if any failed
	var outputs []string
	allSuccess := true
	for _, r := range results {
		run.Results[r.result.StepID] = r.result
		outputs = append(outputs, r.result.Output)
		if r.result.Status != "success" {
			allSuccess = false
		}
	}

	status := "success"
	if !allSuccess {
		status = "failure"
	}

	return StepResult{
		StepID: step.ID, Status: status,
		Output:    strings.Join(outputs, "\n---\n"),
		Duration:  time.Since(start),
		Timestamp: time.Now(),
	}
}

func (e *Engine) executeLoopStep(step Step, run *Run) StepResult {
	start := time.Now()

	items := strings.Split(step.LoopOver, ",")
	if step.LoopOver == "" {
		// Try to get from a previous step's output
		if ref, ok := step.Config["loop_source"]; ok {
			if prev, exists := run.Results[ref]; exists {
				items = strings.Split(prev.Output, "\n")
			}
		}
	}

	bodyStepID := step.Config["body_step"]
	if bodyStepID == "" {
		return StepResult{StepID: step.ID, Status: "failure", Error: "body_step not configured", Duration: time.Since(start), Timestamp: time.Now()}
	}

	// Find body step definition
	var bodyStep Step
	e.mu.RLock()
	wf := e.workflows[run.WorkflowID]
	e.mu.RUnlock()
	if wf != nil {
		for _, s := range wf.Steps {
			if s.ID == bodyStepID {
				bodyStep = s
				break
			}
		}
	}

	var outputs []string
	for i, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		// Substitute {{item}} in the body step config
		iterStep := bodyStep
		iterStep.ID = fmt.Sprintf("%s_iter_%d", step.ID, i)
		newConfig := make(map[string]string)
		for k, v := range iterStep.Config {
			newConfig[k] = strings.ReplaceAll(v, "{{item}}", item)
		}
		iterStep.Config = newConfig

		result := e.executeStep(iterStep, run)
		run.Results[iterStep.ID] = result
		outputs = append(outputs, result.Output)
	}

	return StepResult{
		StepID: step.ID, Status: "success",
		Output:    strings.Join(outputs, "\n"),
		Duration:  time.Since(start),
		Timestamp: time.Now(),
	}
}

func (e *Engine) executeHumanInputStep(step Step, run *Run) StepResult {
	start := time.Now()
	prompt := step.Config["prompt"]
	if prompt == "" {
		prompt = "Waiting for user input..."
	}

	run.Status = RunWaitingInput
	logger.Info("workflow waiting for input", map[string]any{"run_id": run.ID, "prompt": prompt})

	inputCh := make(chan string, 1)
	e.mu.Lock()
	e.waitingInputs[run.ID] = inputCh
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		delete(e.waitingInputs, run.ID)
		e.mu.Unlock()
	}()

	// Wait with timeout
	timeout := 30 * time.Minute
	if t, ok := step.Config["timeout"]; ok {
		if d, err := time.ParseDuration(t); err == nil {
			timeout = d
		}
	}

	select {
	case input := <-inputCh:
		return StepResult{StepID: step.ID, Status: "success", Output: input, Duration: time.Since(start), Timestamp: time.Now()}
	case <-time.After(timeout):
		return StepResult{StepID: step.ID, Status: "failure", Error: "input timeout", Duration: time.Since(start), Timestamp: time.Now()}
	}
}

func (e *Engine) handleSubmitInput(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	runID := params["run_id"]
	input := params["input"]

	e.mu.RLock()
	ch, exists := e.waitingInputs[runID]
	e.mu.RUnlock()

	if !exists {
		return ipc.Message{}, fmt.Errorf("no waiting input for run %q", runID)
	}

	ch <- input
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "submitted"}, nil
}

func (e *Engine) handleTriggerAdd(msg ipc.Message) (ipc.Message, error) {
	trigger, ok := msg.Payload.(Trigger)
	if !ok {
		// Try map
		if m, ok := msg.Payload.(map[string]string); ok {
			trigger = Trigger{Type: m["type"], Pattern: m["pattern"], WorkflowID: m["workflow_id"]}
		} else {
			return ipc.Message{}, fmt.Errorf("expected Trigger, got %T", msg.Payload)
		}
	}
	e.mu.Lock()
	e.triggers = append(e.triggers, trigger)
	e.mu.Unlock()
	logger.Info("workflow trigger added", map[string]any{"type": trigger.Type, "pattern": trigger.Pattern, "workflow_id": trigger.WorkflowID})
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "added"}, nil
}

func (e *Engine) handleTriggerList(msg ipc.Message) (ipc.Message, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: e.triggers}, nil
}

func (e *Engine) failRun(run *Run, errMsg string) {
	logger.Error("workflow run failed", map[string]any{"run_id": run.ID, "error": errMsg})
	e.mu.Lock()
	run.Status = RunFailed
	run.Error = errMsg
	run.CompletedAt = time.Now().UTC()
	e.mu.Unlock()
}

func substituteVar(s, old, new string) string {
	result := s
	for {
		idx := indexOf(result, old)
		if idx < 0 {
			break
		}
		result = result[:idx] + new + result[idx+len(old):]
	}
	return result
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
