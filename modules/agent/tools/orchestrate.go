package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
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
		"tasks":          {Type: "string", Description: `JSON array of tasks: [{"agent":"a","message":"m"}]. Agents run in your tenant.`, Required: true},
		"shared_context": {Type: "string", Description: `Optional JSON object of notes to share with every child, e.g. {"plan":"...","schema":"..."}. As the coordinator you write it; the children read it read-only via context_read. Use it to hand a plan or intermediate result to the workers.`, Required: false},
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
	tenant, agentName, user := agent.ToolCaller(ctx)
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

	// Parse shared context up front: a malformed request is the coordinator's
	// mistake and nothing has been written or spawned yet, so fail fast with
	// clear feedback. A WRITE failure later is different — see writeSharedContext.
	notes, err := parseSharedContext(input["shared_context"])
	if err != nil {
		return "", err
	}

	// One correlation id for the whole fan-out so every child's audit trail
	// links back to this orchestration. It is also the shared-context channel
	// id: children receive it as their TraceID and read this batch's notes
	// through it (#48).
	batchTrace := genTraceID()

	// As the coordinator, write any shared context BEFORE fanning out so the
	// children can read it. Children are never given a write tool, so the
	// channel is writable only here (coordinator) and read-only for workers.
	// This is best-effort: if the store is disabled or a write fails, the
	// orchestration still runs — workers just see no shared context rather than
	// the whole batch failing.
	t.writeSharedContext(ctx, tenant, agentName, batchTrace, notes)

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

// parseSharedContext decodes the optional shared_context object. It is tolerant
// of value types: a string is used verbatim; any other JSON scalar/object is
// rendered as its raw JSON text, so {"steps":3} or {"cfg":{...}} no longer
// aborts the call. Only a payload that is not a JSON object at all is an error.
func parseSharedContext(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil, fmt.Errorf("invalid shared_context JSON (want an object, e.g. {\"plan\":\"...\"}): %w", err)
	}
	notes := make(map[string]string, len(obj))
	for key, val := range obj {
		if key == "" {
			continue
		}
		var s string
		if json.Unmarshal(val, &s) == nil {
			notes[key] = s // it was a JSON string
		} else {
			notes[key] = string(val) // keep the raw JSON for non-string values
		}
	}
	return notes, nil
}

// writeSharedContext writes each note into the batch's channel through the
// runtime, recording the coordinator agent as author. It is best-effort: a
// write failure (store disabled, transient db error) is swallowed per key so a
// valid orchestration is never killed by the shared-context handoff. A single
// timeout context covers the whole batch of writes.
func (t *OrchestrateTool) writeSharedContext(ctx context.Context, tenant, author, channel string, notes map[string]string) {
	if len(notes) == 0 {
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for key, content := range notes {
		t.bus.Request(writeCtx, ipc.Message{
			Source:  "orchestrate",
			Target:  "agent_runtime",
			Topic:   agent.TopicContextWrite,
			TraceID: channel,
			Payload: agent.SharedContextEntry{
				Tenant: tenant, Channel: channel, Key: key, Content: content, Author: author,
			},
		})
	}
}

// orchSeq guarantees a unique fallback channel id even if crypto/rand fails.
var orchSeq atomic.Uint64

func genTraceID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// Never return a constant: this id is also the shared-context channel
		// key, so a fixed fallback would collapse concurrent batches onto one
		// channel and cross-read their notes (#48).
		return fmt.Sprintf("orch-fb-%d-%d", time.Now().UnixNano(), orchSeq.Add(1))
	}
	return "orch-" + hex.EncodeToString(buf)
}
