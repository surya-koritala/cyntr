package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/usermodel"
)

// UserModelReadTool reads the curated profile + preferences for the calling
// user. The (tenant, user) pair is pulled from the tool-call context so the
// agent cannot impersonate someone else.
type UserModelReadTool struct {
	bus *ipc.Bus
}

// NewUserModelReadTool constructs a UserModelReadTool. bus is the IPC bus
// used to talk to the usermodel module.
func NewUserModelReadTool(bus *ipc.Bus) *UserModelReadTool {
	return &UserModelReadTool{bus: bus}
}

func (t *UserModelReadTool) Name() string { return "user_model_read" }

func (t *UserModelReadTool) Description() string {
	return `Read the curated profile and preferences for the current user. Returns two markdown sections (profile + preferences) drawn from the per-user profile store. Use this when you need durable context about who the user is, what they're working on, or how they like to be addressed — distinct from the flat "memories" stream.`
}

func (t *UserModelReadTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{}
}

func (t *UserModelReadTool) Execute(ctx context.Context, _ map[string]string) (string, error) {
	tenant, _, user := agent.ToolCaller(ctx)
	if tenant == "" || user == "" {
		return "", fmt.Errorf("user_model_read: no tenant/user in tool context")
	}
	if t.bus == nil {
		return "", fmt.Errorf("user_model_read: bus not configured")
	}

	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := t.bus.Request(callCtx, ipc.Message{
		Source: "user_model_read", Target: "usermodel", Topic: usermodel.TopicGet,
		Payload: map[string]string{"tenant": tenant, "user": user},
	})
	if err != nil {
		if err == ipc.ErrNoHandler {
			return "(no user model module registered)", nil
		}
		return "", fmt.Errorf("user_model_read: %w", err)
	}
	p, ok := resp.Payload.(usermodel.UserProfile)
	if !ok {
		return "", fmt.Errorf("user_model_read: unexpected payload %T", resp.Payload)
	}
	return formatProfile(p), nil
}

// formatProfile renders a UserProfile as a clearly labeled two-section
// markdown block. Empty sections are still labeled so the agent can see that
// the section exists but is currently blank.
func formatProfile(p usermodel.UserProfile) string {
	var b strings.Builder
	b.WriteString("# User profile\n\n")
	if strings.TrimSpace(p.ProfileMD) == "" {
		b.WriteString("(empty — no profile written yet)\n")
	} else {
		b.WriteString(p.ProfileMD)
		if !strings.HasSuffix(p.ProfileMD, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n# User preferences\n\n")
	if strings.TrimSpace(p.PreferencesMD) == "" {
		b.WriteString("(empty — no preferences written yet)\n")
	} else {
		b.WriteString(p.PreferencesMD)
		if !strings.HasSuffix(p.PreferencesMD, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}
