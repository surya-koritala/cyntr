package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

// ContextReadTool lets a worker subagent read the shared context its
// coordinator wrote for the current orchestration (#48). It is read-only by
// construction: there is no companion write tool, so a worker can never modify
// the channel. The channel id and tenant are taken from the runtime-supplied
// tool context — never from the model — so a worker can only ever read its own
// batch's notes within its own tenant.
type ContextReadTool struct {
	bus *ipc.Bus
}

func NewContextReadTool(bus *ipc.Bus) *ContextReadTool { return &ContextReadTool{bus: bus} }

func (t *ContextReadTool) Name() string { return "context_read" }
func (t *ContextReadTool) Description() string {
	return "Read the shared context the coordinator set for this task (e.g. a plan or intermediate result). Read-only. Returns nothing if you were not spawned as part of an orchestration."
}
func (t *ContextReadTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{}
}

func (t *ContextReadTool) Execute(ctx context.Context, _ map[string]string) (string, error) {
	if t.bus == nil {
		return "", fmt.Errorf("context_read: bus not configured")
	}
	tenant, _, _ := agent.ToolCaller(ctx)
	channel := agent.Channel(ctx)
	if tenant == "" || channel == "" {
		// Not part of an orchestration — no shared channel to read.
		return agent.FormatSharedContext(nil), nil
	}

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := t.bus.Request(reqCtx, ipc.Message{
		Source:  "context_read",
		Target:  "agent_runtime",
		Topic:   agent.TopicContextRead,
		Payload: agent.ContextReadRequest{Tenant: tenant, Channel: channel},
	})
	if err != nil {
		return "", fmt.Errorf("context_read: %w", err)
	}
	res, ok := resp.Payload.(agent.ContextReadResult)
	if !ok {
		return "", fmt.Errorf("context_read: unexpected response type %T", resp.Payload)
	}
	return agent.FormatSharedContext(res.Entries), nil
}
