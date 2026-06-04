package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

// OrchestrateTool fans a turn out to several child agents that run in
// parallel, then collects their results. Children are isolated: each runs in
// its own session, and — critically — always in the CALLER's tenant and under
// the caller's user, regardless of what the model names in the task list. A
// model can therefore never reach another tenant's agents through this tool.
type OrchestrateTool struct {
	bus            *ipc.Bus
	maxConcurrency int // bounded worker pool
	maxFanout      int // hard cap on children per call
}

func NewOrchestrateTool(bus *ipc.Bus) *OrchestrateTool {
	return &OrchestrateTool{bus: bus, maxConcurrency: 8, maxFanout: 10}
}

func (t *OrchestrateTool) Name() string { return "orchestrate_agents" }
func (t *OrchestrateTool) Description() string {
	return "Delegate tasks to multiple agents in parallel and collect their results. Useful for gathering information from different specialists simultaneously. All child agents run in your own tenant."
}
func (t *OrchestrateTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"tasks": {Type: "string", Description: `JSON array of tasks: [{"agent":"a","message":"m"}]. Agents run in your tenant.`, Required: true},
	}
}

type orchestrateTask struct {
	// Tenant is accepted for backward compatibility but ignored — children
	// always run in the caller's tenant.
	Tenant  string `json:"tenant"`
	Agent   string `json:"agent"`
	Message string `json:"message"`
}

type orchestrateResult struct {
	Agent   string `json:"agent"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

func (t *OrchestrateTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	tasksJSON := input["tasks"]
	if tasksJSON == "" {
		return "", fmt.Errorf("tasks is required")
	}
	if t.bus == nil {
		return "", fmt.Errorf("orchestrate: bus not configured")
	}

	// Children inherit the caller's tenant + user from the tool context, so
	// the model cannot fan out into another tenant.
	tenant, _, user := agent.ToolCaller(ctx)
	if tenant == "" {
		return "", fmt.Errorf("orchestrate: no tenant in tool context")
	}

	var tasks []orchestrateTask
	if err := json.Unmarshal([]byte(tasksJSON), &tasks); err != nil {
		return "", fmt.Errorf("invalid tasks JSON: %w", err)
	}
	if len(tasks) == 0 {
		return "No tasks provided.", nil
	}
	if len(tasks) > t.maxFanout {
		return "", fmt.Errorf("maximum %d parallel tasks allowed", t.maxFanout)
	}

	// One correlation id for the whole fan-out so every child's audit trail
	// links back to this orchestration.
	batchTrace := genTraceID()

	results := make([]orchestrateResult, len(tasks))
	sem := make(chan struct{}, t.maxConcurrency)
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, tk orchestrateTask) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			taskCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
			defer cancel()

			resp, err := t.bus.Request(taskCtx, ipc.Message{
				Source:  "orchestrate",
				Target:  "agent_runtime",
				Topic:   "agent.chat",
				TraceID: batchTrace,
				Payload: agent.ChatRequest{Agent: tk.Agent, Tenant: tenant, User: user, Message: tk.Message},
			})
			if err != nil {
				results[idx] = orchestrateResult{Agent: tk.Agent, Error: err.Error()}
				return
			}
			chatResp, ok := resp.Payload.(agent.ChatResponse)
			if !ok {
				results[idx] = orchestrateResult{Agent: tk.Agent, Error: "unexpected response type"}
				return
			}
			results[idx] = orchestrateResult{Agent: tk.Agent, Content: chatResp.Content}
		}(i, task)
	}
	wg.Wait()

	var sb strings.Builder
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("## Agent: %s\n", r.Agent))
		if r.Error != "" {
			sb.WriteString(fmt.Sprintf("Error: %s\n", r.Error))
		} else {
			sb.WriteString(r.Content)
		}
		sb.WriteString("\n\n---\n\n")
	}
	return sb.String(), nil
}

func genTraceID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "orch"
	}
	return "orch-" + hex.EncodeToString(buf)
}
