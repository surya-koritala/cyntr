package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/usermodel"
)

// UserModelWriteTool overwrites either the profile or preferences section of
// the calling user's curated profile. The (tenant, user) pair is pulled from
// the tool-call context — the agent cannot target someone else's profile.
type UserModelWriteTool struct {
	bus *ipc.Bus
}

// NewUserModelWriteTool constructs a UserModelWriteTool wired to bus.
func NewUserModelWriteTool(bus *ipc.Bus) *UserModelWriteTool {
	return &UserModelWriteTool{bus: bus}
}

func (t *UserModelWriteTool) Name() string { return "user_model_write" }

func (t *UserModelWriteTool) Description() string {
	return `Overwrite one section of the current user's curated profile. Use this sparingly — the section you write replaces what was there, so include everything you want preserved. Limit: 4096 bytes per section.

Params:
  section: "profile" or "preferences"
  content: full markdown content for that section`
}

func (t *UserModelWriteTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"section": {Type: "string", Description: `"profile" or "preferences"`, Required: true},
		"content": {Type: "string", Description: "Markdown content (max 4096 bytes)", Required: true},
	}
}

func (t *UserModelWriteTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	tenant, _, user := agent.ToolCaller(ctx)
	if tenant == "" || user == "" {
		return "", fmt.Errorf("user_model_write: no tenant/user in tool context")
	}
	if t.bus == nil {
		return "", fmt.Errorf("user_model_write: bus not configured")
	}

	section := input["section"]
	content := input["content"]
	if content == "" {
		return "", fmt.Errorf("user_model_write: content is required")
	}
	if len(content) > usermodel.MaxSectionBytes {
		return "", fmt.Errorf("user_model_write: content exceeds %d bytes", usermodel.MaxSectionBytes)
	}

	var topic string
	switch section {
	case "profile":
		topic = usermodel.TopicUpsertProfile
	case "preferences":
		topic = usermodel.TopicUpsertPreferences
	default:
		return "", fmt.Errorf("user_model_write: section must be \"profile\" or \"preferences\", got %q", section)
	}

	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_, err := t.bus.Request(callCtx, ipc.Message{
		Source: "user_model_write", Target: "usermodel", Topic: topic,
		Payload: map[string]string{"tenant": tenant, "user": user, "md": content},
	})
	if err != nil {
		if err == ipc.ErrNoHandler {
			return "", fmt.Errorf("user_model_write: usermodel module not registered")
		}
		return "", fmt.Errorf("user_model_write: %w", err)
	}
	return fmt.Sprintf("ok: updated %s section for %s/%s", section, tenant, user), nil
}
