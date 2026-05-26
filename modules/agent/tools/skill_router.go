package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/curator"
)

// SkillRouterTool allows agents to dynamically load skill instructions mid-conversation.
type SkillRouterTool struct {
	bus *ipc.Bus
}

func NewSkillRouterTool(bus *ipc.Bus) *SkillRouterTool {
	return &SkillRouterTool{bus: bus}
}

func (t *SkillRouterTool) Name() string { return "skill_router" }
func (t *SkillRouterTool) Description() string {
	return "Load skill instructions on-demand. Use 'list' to see available skills, or provide a skill name to load its full instructions. Skills provide specialized knowledge for specific tasks like code review, incident response, security auditing, etc."
}
func (t *SkillRouterTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"action":     {Type: "string", Description: "Action: 'list' to see available skills, 'load' to load a specific skill", Required: true},
		"skill_name": {Type: "string", Description: "Name of the skill to load (required for 'load' action)", Required: false},
	}
}

func (t *SkillRouterTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	action := input["action"]

	switch action {
	case "list":
		resp, err := t.bus.Request(ctx, ipc.Message{
			Source: "skill_router", Target: "skill_runtime", Topic: "skill.list",
		})
		if err != nil {
			return "", fmt.Errorf("list skills: %w", err)
		}
		names, ok := resp.Payload.([]string)
		if !ok || len(names) == 0 {
			return "No skills installed.", nil
		}
		return "Available skills:\n- " + strings.Join(names, "\n- "), nil

	case "load":
		skillName := input["skill_name"]
		if skillName == "" {
			return "", fmt.Errorf("skill_name is required for 'load' action")
		}
		// Wrap the inner call so we can record the outcome for the
		// curator. The publish is fire-and-forget; it must not add
		// latency to the agent loop.
		start := time.Now()
		out, execErr := t.loadSkill(ctx, skillName)
		t.recordOutcome(skillName, input, execErr, time.Since(start))
		return out, execErr

	default:
		return "", fmt.Errorf("unknown action %q: use 'list' or 'load'", action)
	}
}

// loadSkill is the inner load implementation, factored out so the
// outer Execute can capture timing + success/error for curator
// recording.
func (t *SkillRouterTool) loadSkill(ctx context.Context, skillName string) (string, error) {
	resp, err := t.bus.Request(ctx, ipc.Message{
		Source: "skill_router", Target: "skill_runtime", Topic: "skill.instructions",
		Payload: []string{skillName},
	})
	if err != nil {
		return "", fmt.Errorf("load skill: %w", err)
	}
	instructions, ok := resp.Payload.(map[string]string)
	if !ok {
		return "", fmt.Errorf("unexpected response type")
	}
	if instr, found := instructions[skillName]; found {
		return fmt.Sprintf("# Skill: %s\n\n%s", skillName, instr), nil
	}
	return "", fmt.Errorf("skill %q not found", skillName)
}

// recordOutcome fires a curator.record event on the bus. It is
// best-effort: if the bus is nil (tests) or no subscriber exists,
// the publish is silently dropped. Either way we never block on it.
func (t *SkillRouterTool) recordOutcome(skillName string, input map[string]string, execErr error, dur time.Duration) {
	if t.bus == nil {
		return
	}
	errStr := ""
	if execErr != nil {
		errStr = execErr.Error()
	}
	inv := curator.Invocation{
		SkillName:  skillName,
		Tenant:     input["tenant"],
		Agent:      input["agent"],
		Success:    execErr == nil,
		Error:      errStr,
		DurationMs: dur.Milliseconds(),
		Timestamp:  time.Now().UTC(),
	}
	// fire-and-forget — Publish returns immediately and subscribers
	// run in their own goroutines.
	_ = t.bus.Publish(ipc.Message{
		Source: "skill_router",
		Type:   ipc.MessageTypeEvent,
		Topic:  curator.TopicRecord,
		Payload: inv,
	})
}
