package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

// DelegateTool allows an agent to delegate tasks to other agents.
type DelegateTool struct {
	bus *ipc.Bus
}

func NewDelegateTool(bus *ipc.Bus) *DelegateTool {
	return &DelegateTool{bus: bus}
}

func (t *DelegateTool) Name() string { return "delegate_agent" }
func (t *DelegateTool) Description() string {
	return "Delegate a task to another agent and get their response. Use this when a specialized agent would handle the task better."
}
func (t *DelegateTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"tenant":  {Type: "string", Description: "Tenant of the target agent", Required: true},
		"agent":   {Type: "string", Description: "Name of the agent to delegate to", Required: true},
		"message": {Type: "string", Description: "The task/question to delegate", Required: true},
	}
}

func (t *DelegateTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	tenant := input["tenant"]
	agentName := input["agent"]
	message := input["message"]

	if tenant == "" || agentName == "" || message == "" {
		return "", fmt.Errorf("tenant, agent, and message are required")
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	resp, err := t.bus.Request(ctx, ipc.Message{
		Source: "delegate_tool",
		Target: "agent_runtime",
		Topic:  "agent.chat",
		Payload: agent.ChatRequest{
			Agent:   agentName,
			Tenant:  tenant,
			User:    "delegate",
			Message: message,
		},
	})
	if err != nil {
		return "", fmt.Errorf("delegation failed: %w", err)
	}

	chatResp, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		return "", fmt.Errorf("unexpected response type: %T", resp.Payload)
	}

	return fmt.Sprintf("Response from %s:\n\n%s", agentName, chatResp.Content), nil
}
