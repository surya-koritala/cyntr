package crew

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

var logger = log.Default().WithModule("crew")

// Engine manages multi-agent crews.
type Engine struct {
	bus     *ipc.Bus
	crews   map[string]*Crew
	runs    map[string]*CrewRun
	mu      sync.RWMutex
	counter int64
}

func New() *Engine {
	return &Engine{
		crews: make(map[string]*Crew),
		runs:  make(map[string]*CrewRun),
	}
}

func (e *Engine) Name() string           { return "crew" }
func (e *Engine) Dependencies() []string { return []string{"agent_runtime"} }

func (e *Engine) Init(ctx context.Context, svc *kernel.Services) error {
	e.bus = svc.Bus
	return nil
}

func (e *Engine) Start(ctx context.Context) error {
	e.bus.Handle("crew", "crew.create", e.handleCreate)
	e.bus.Handle("crew", "crew.run", e.handleRun)
	e.bus.Handle("crew", "crew.status", e.handleStatus)
	e.bus.Handle("crew", "crew.list", e.handleList)
	e.bus.Handle("crew", "crew.list_runs", e.handleListRuns)
	return nil
}

func (e *Engine) Stop(ctx context.Context) error { return nil }

func (e *Engine) Health(ctx context.Context) kernel.HealthStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return kernel.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("%d crews, %d runs", len(e.crews), len(e.runs)),
	}
}

func (e *Engine) handleCreate(msg ipc.Message) (ipc.Message, error) {
	crew, ok := msg.Payload.(Crew)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected Crew, got %T", msg.Payload)
	}
	e.counter++
	if crew.ID == "" {
		crew.ID = fmt.Sprintf("crew_%d", e.counter)
	}
	if crew.Mode == "" {
		crew.Mode = "pipeline"
	}

	e.mu.Lock()
	e.crews[crew.ID] = &crew
	e.mu.Unlock()

	logger.Info("crew created", map[string]any{"id": crew.ID, "name": crew.Name, "members": len(crew.Members)})
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: map[string]string{"crew_id": crew.ID}}, nil
}

func (e *Engine) handleRun(msg ipc.Message) (ipc.Message, error) {
	params, ok := msg.Payload.(map[string]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected map[string]string, got %T", msg.Payload)
	}
	crewID := params["crew_id"]
	input := params["input"]

	e.mu.RLock()
	crew, exists := e.crews[crewID]
	e.mu.RUnlock()
	if !exists {
		return ipc.Message{}, fmt.Errorf("crew %q not found", crewID)
	}

	e.counter++
	run := &CrewRun{
		ID:        fmt.Sprintf("crun_%d", e.counter),
		CrewID:    crewID,
		Status:    "running",
		Input:     input,
		Results:   make(map[string]string),
		StartedAt: time.Now(),
	}

	e.mu.Lock()
	e.runs[run.ID] = run
	e.mu.Unlock()

	// Execute async
	go e.executeCrew(crew, run)

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: map[string]string{"run_id": run.ID}}, nil
}

func (e *Engine) executeCrew(crew *Crew, run *CrewRun) {
	logger.Info("crew execution started", map[string]any{"crew": crew.Name, "run": run.ID, "mode": crew.Mode})

	switch crew.Mode {
	case "pipeline":
		e.executePipeline(crew, run)
	case "parallel":
		e.executeParallel(crew, run)
	case "sequential":
		e.executeSequential(crew, run)
	default:
		run.Error = "unknown mode: " + crew.Mode
		run.Status = "failed"
	}

	if run.Error == "" {
		run.Status = "completed"
	}
	run.CompletedAt = time.Now()
	logger.Info("crew execution completed", map[string]any{"crew": crew.Name, "run": run.ID, "status": run.Status})
}

func (e *Engine) executePipeline(crew *Crew, run *CrewRun) {
	currentInput := run.Input

	for _, member := range crew.Members {
		// Build message: include role, goal, and previous output
		message := fmt.Sprintf("Your role: %s\nYour goal: %s\n\nInput:\n%s", member.Role, member.Goal, currentInput)

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		resp, err := e.bus.Request(ctx, ipc.Message{
			Source: "crew", Target: "agent_runtime", Topic: "agent.chat",
			Payload: agent.ChatRequest{
				Agent:   member.Agent,
				Tenant:  crew.Tenant,
				User:    "crew:" + crew.Name,
				Message: message,
			},
		})
		cancel()

		if err != nil {
			run.Error = fmt.Sprintf("agent %s failed: %s", member.Agent, err.Error())
			run.Status = "failed"
			logger.Error("crew member failed", map[string]any{"agent": member.Agent, "error": err.Error()})
			return
		}

		chatResp, ok := resp.Payload.(agent.ChatResponse)
		if !ok {
			run.Error = fmt.Sprintf("unexpected response from %s", member.Agent)
			run.Status = "failed"
			return
		}

		run.Results[member.Agent] = chatResp.Content
		currentInput = chatResp.Content // pipe output to next agent
	}

	// Final output is the last agent's response
	if len(crew.Members) > 0 {
		lastAgent := crew.Members[len(crew.Members)-1].Agent
		run.Output = run.Results[lastAgent]
	}
}

func (e *Engine) executeParallel(crew *Crew, run *CrewRun) {
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, member := range crew.Members {
		wg.Add(1)
		go func(m CrewMember) {
			defer wg.Done()

			message := fmt.Sprintf("Your role: %s\nYour goal: %s\n\nTask:\n%s", m.Role, m.Goal, run.Input)

			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			resp, err := e.bus.Request(ctx, ipc.Message{
				Source: "crew", Target: "agent_runtime", Topic: "agent.chat",
				Payload: agent.ChatRequest{Agent: m.Agent, Tenant: crew.Tenant, User: "crew:" + crew.Name, Message: message},
			})
			cancel()

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				run.Results[m.Agent] = "ERROR: " + err.Error()
				return
			}
			if chatResp, ok := resp.Payload.(agent.ChatResponse); ok {
				run.Results[m.Agent] = chatResp.Content
			}
		}(member)
	}

	wg.Wait()

	// Aggregate all outputs
	var outputs []string
	for _, m := range crew.Members {
		if result, ok := run.Results[m.Agent]; ok {
			outputs = append(outputs, fmt.Sprintf("## %s (%s)\n%s", m.Agent, m.Role, result))
		}
	}
	run.Output = strings.Join(outputs, "\n\n---\n\n")
}

func (e *Engine) executeSequential(crew *Crew, run *CrewRun) {
	var outputs []string

	for _, member := range crew.Members {
		message := fmt.Sprintf("Your role: %s\nYour goal: %s\n\nTask:\n%s", member.Role, member.Goal, run.Input)

		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		resp, err := e.bus.Request(ctx, ipc.Message{
			Source: "crew", Target: "agent_runtime", Topic: "agent.chat",
			Payload: agent.ChatRequest{Agent: member.Agent, Tenant: crew.Tenant, User: "crew:" + crew.Name, Message: message},
		})
		cancel()

		if err != nil {
			run.Results[member.Agent] = "ERROR: " + err.Error()
			continue
		}
		if chatResp, ok := resp.Payload.(agent.ChatResponse); ok {
			run.Results[member.Agent] = chatResp.Content
			outputs = append(outputs, fmt.Sprintf("## %s (%s)\n%s", member.Agent, member.Role, chatResp.Content))
		}
	}

	run.Output = strings.Join(outputs, "\n\n---\n\n")
}

func (e *Engine) handleStatus(msg ipc.Message) (ipc.Message, error) {
	runID, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string, got %T", msg.Payload)
	}
	e.mu.RLock()
	run, exists := e.runs[runID]
	e.mu.RUnlock()
	if !exists {
		return ipc.Message{}, fmt.Errorf("run %q not found", runID)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: run}, nil
}

func (e *Engine) handleList(msg ipc.Message) (ipc.Message, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var crews []Crew
	for _, c := range e.crews {
		crews = append(crews, *c)
	}
	if crews == nil {
		crews = []Crew{}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: crews}, nil
}

func (e *Engine) handleListRuns(msg ipc.Message) (ipc.Message, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var runs []CrewRun
	for _, r := range e.runs {
		runs = append(runs, *r)
	}
	if runs == nil {
		runs = []CrewRun{}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: runs}, nil
}
