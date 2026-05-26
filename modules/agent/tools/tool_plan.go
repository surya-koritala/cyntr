package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

// ToolPlanTool executes a multi-step plan of tool calls in a single LLM turn.
// Each step can reference outputs from earlier steps via ${steps.<id>} and
// top-level input vars via ${vars.<key>}. Every sub-call is run through the
// policy engine just like a normal tool dispatch.
type ToolPlanTool struct {
	registry     *agent.ToolRegistry
	bus          *ipc.Bus
	maxSteps     int
	totalTimeout time.Duration
}

// NewToolPlanTool constructs a ToolPlanTool. The bus may be nil in tests; when
// nil, policy checks are skipped (mirroring the runtime's behavior when no
// policy module is registered).
func NewToolPlanTool(registry *agent.ToolRegistry, bus *ipc.Bus) *ToolPlanTool {
	return &ToolPlanTool{
		registry:     registry,
		bus:          bus,
		maxSteps:     20,
		totalTimeout: 5 * time.Minute,
	}
}

func (t *ToolPlanTool) Name() string { return "tool_plan" }

func (t *ToolPlanTool) Description() string {
	return `Execute a multi-step plan of tool calls in one turn. Use this when you need to chain 2+ tool calls where later steps depend on earlier outputs — it saves the token round-trip of calling each tool separately.

Plan format (JSON):
{"steps":[{"id":"<step-id>","tool":"<tool-name>","params":{"<param>":"<value>"}}, ...]}

Reference prior step outputs with ${steps.<id>} in any param value.
Reference top-level input vars with ${vars.<key>} (the optional 'vars' input is a JSON map).

Example — fetch a URL, parse a field out of the JSON, then query a database with it, all in one call:
{"steps":[
  {"id":"fetch","tool":"http_request","params":{"url":"https://api.example.com/u/42"}},
  {"id":"parse","tool":"json_query","params":{"json_data":"${steps.fetch}","path":"email"}},
  {"id":"lookup","tool":"database_query","params":{"query":"SELECT name FROM users WHERE email='${steps.parse}'"}}
]}

Returns a JSON object: {"steps":{"<id>":"<output>",...},"final":"<last step output>"}.
Limits: max 20 steps per plan, 5-minute total timeout. tool_plan cannot recursively call tool_plan.`
}

func (t *ToolPlanTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"plan": {Type: "string", Description: `JSON plan: {"steps":[{"id":...,"tool":...,"params":{...}}]}`, Required: true},
		"vars": {Type: "string", Description: "Optional JSON map of input variables referenced via ${vars.<key>}", Required: false},
	}
}

type planStep struct {
	ID     string            `json:"id"`
	Tool   string            `json:"tool"`
	Params map[string]string `json:"params"`
}

type plan struct {
	Steps []planStep `json:"steps"`
}

type planResult struct {
	Steps map[string]string `json:"steps"`
	Final string            `json:"final"`
}

var refPattern = regexp.MustCompile(`\$\{(steps|vars)\.([^}]+)\}`)

func (t *ToolPlanTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	planJSON := strings.TrimSpace(input["plan"])
	if planJSON == "" {
		return "", fmt.Errorf("plan is required")
	}

	var p plan
	if err := json.Unmarshal([]byte(planJSON), &p); err != nil {
		return "", fmt.Errorf("invalid plan JSON: %w", err)
	}

	if len(p.Steps) == 0 {
		return "", fmt.Errorf("plan has no steps")
	}
	if len(p.Steps) > t.maxSteps {
		return "", fmt.Errorf("plan has %d steps; max %d allowed", len(p.Steps), t.maxSteps)
	}

	vars := make(map[string]string)
	if varsJSON := strings.TrimSpace(input["vars"]); varsJSON != "" {
		if err := json.Unmarshal([]byte(varsJSON), &vars); err != nil {
			return "", fmt.Errorf("invalid vars JSON: %w", err)
		}
	}

	planCtx, cancel := context.WithTimeout(ctx, t.totalTimeout)
	defer cancel()

	tenant, agentName, user := agent.ToolCaller(ctx)

	stepOutputs := make(map[string]string, len(p.Steps))
	seen := make(map[string]bool, len(p.Steps))
	var lastOutput string

	for i, step := range p.Steps {
		if step.ID == "" {
			return "", fmt.Errorf("step %d missing id", i)
		}
		if seen[step.ID] {
			return "", fmt.Errorf("duplicate step id %q", step.ID)
		}
		seen[step.ID] = true

		if step.Tool == "" {
			return "", fmt.Errorf("step %q missing tool", step.ID)
		}
		if step.Tool == "tool_plan" {
			return "", fmt.Errorf("step %q: tool_plan cannot recursively call itself", step.ID)
		}

		// Substitute ${steps.<id>} and ${vars.<key>} in every param value.
		resolved := make(map[string]string, len(step.Params))
		for k, v := range step.Params {
			sub, err := substituteRefs(v, stepOutputs, vars)
			if err != nil {
				return formatPartial(stepOutputs, lastOutput),
					fmt.Errorf("step %q param %q: %w", step.ID, k, err)
			}
			resolved[k] = sub
		}

		// Enforce the policy engine on every sub-call. If the calling agent
		// isn't allowed to use the tool directly, it isn't allowed to use it
		// via tool_plan either.
		if decision := t.checkPolicy(planCtx, tenant, agentName, user, step.Tool); decision != "" {
			switch decision {
			case "deny":
				return formatPartial(stepOutputs, lastOutput),
					fmt.Errorf("step %q (%s): policy denied", step.ID, step.Tool)
			case "require_approval":
				// In a one-shot plan we don't support the human-in-the-loop
				// approval flow (the runtime owns that). Fail fast so the
				// caller knows to invoke the tool directly to get an approval
				// prompt rather than silently bypassing it.
				return formatPartial(stepOutputs, lastOutput),
					fmt.Errorf("step %q (%s): tool requires approval — call it directly instead of via tool_plan", step.ID, step.Tool)
			}
		}

		out, err := t.registry.Execute(planCtx, step.Tool, resolved)
		if err != nil {
			return formatPartial(stepOutputs, lastOutput),
				fmt.Errorf("step %q (%s) failed: %w", step.ID, step.Tool, err)
		}

		stepOutputs[step.ID] = out
		lastOutput = out
	}

	return marshalResult(planResult{Steps: stepOutputs, Final: lastOutput}), nil
}

// checkPolicy returns "deny", "require_approval", "allow", or "" if no policy
// module is registered (matching the runtime's fail-open semantics for the
// no-policy case).
func (t *ToolPlanTool) checkPolicy(ctx context.Context, tenant, agentName, user, toolName string) string {
	if t.bus == nil {
		return ""
	}
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resp, err := t.bus.Request(callCtx, ipc.Message{
		Source: "tool_plan", Target: "policy", Topic: "policy.check",
		Payload: policy.CheckRequest{
			Tenant: tenant, Action: "tool_call", Tool: toolName,
			Agent: agentName, User: user,
		},
	})
	if err != nil {
		if err == ipc.ErrNoHandler {
			return ""
		}
		return "deny" // policy module present but errored: fail-closed
	}
	checkResp, ok := resp.Payload.(policy.CheckResponse)
	if !ok {
		return "deny"
	}
	return checkResp.Decision.String()
}

func formatPartial(steps map[string]string, final string) string {
	return marshalResult(planResult{Steps: steps, Final: final})
}

// marshalResult JSON-encodes the plan result without HTML-escaping. The
// default json.Marshal/MarshalIndent escapes `<`, `>`, and `&` to \uXXXX,
// which is ugly when the output is shown to the LLM or returned to the user.
func marshalResult(r planResult) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Sprintf(`{"error":%q}`, err.Error())
	}
	// Encoder appends a trailing newline; strip it for parity with MarshalIndent.
	return strings.TrimRight(buf.String(), "\n")
}

func substituteRefs(s string, steps, vars map[string]string) (string, error) {
	var unresolved error
	result := refPattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := refPattern.FindStringSubmatch(match)
		if len(sub) != 3 {
			return match
		}
		scope, key := sub[1], sub[2]
		switch scope {
		case "steps":
			v, ok := steps[key]
			if !ok {
				unresolved = fmt.Errorf("unknown step reference %q (only resolved earlier steps can be referenced)", key)
				return match
			}
			return v
		case "vars":
			v, ok := vars[key]
			if !ok {
				unresolved = fmt.Errorf("unknown vars reference %q", key)
				return match
			}
			return v
		}
		return match
	})
	if unresolved != nil {
		return "", unresolved
	}
	return result, nil
}
