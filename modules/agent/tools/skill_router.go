package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
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
		return fmt.Sprintf("Skill %q not found. Use action='list' to see available skills.", skillName), nil

	default:
		return "", fmt.Errorf("unknown action %q: use 'list' or 'load'", action)
	}
}
