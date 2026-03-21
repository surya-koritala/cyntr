package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

type OrchestrateTool struct {
	bus *ipc.Bus
}

func NewOrchestrateTool(bus *ipc.Bus) *OrchestrateTool {
	return &OrchestrateTool{bus: bus}
}

func (t *OrchestrateTool) Name() string { return "orchestrate_agents" }
func (t *OrchestrateTool) Description() string {
	return "Delegate tasks to multiple agents in parallel and collect their results. Useful for gathering information from different specialists simultaneously."
}
func (t *OrchestrateTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"tasks": {Type: "string", Description: `JSON array of tasks: [{"tenant":"t","agent":"a","message":"m"}]`, Required: true},
	}
}

type orchestrateTask struct {
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

	var tasks []orchestrateTask
	if err := json.Unmarshal([]byte(tasksJSON), &tasks); err != nil {
		return "", fmt.Errorf("invalid tasks JSON: %w", err)
	}

	if len(tasks) == 0 {
		return "No tasks provided.", nil
	}

	if len(tasks) > 10 {
		return "", fmt.Errorf("maximum 10 parallel tasks allowed")
	}

	results := make([]orchestrateResult, len(tasks))
	var wg sync.WaitGroup

	for i, task := range tasks {
		wg.Add(1)
		go func(idx int, tk orchestrateTask) {
			defer wg.Done()

			taskCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
			defer cancel()

			resp, err := t.bus.Request(taskCtx, ipc.Message{
				Source:  "orchestrate",
				Target:  "agent_runtime",
				Topic:   "agent.chat",
				Payload: agent.ChatRequest{Agent: tk.Agent, Tenant: tk.Tenant, Message: tk.Message},
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

	// Format results
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
