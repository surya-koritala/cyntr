package providers

import (
	"context"
	"sync/atomic"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// Mock is a deterministic model provider for testing.
type Mock struct {
	response string
	toolCall *agent.ToolCall
	// callCount is atomic because a single Mock may serve concurrent chats
	// (it is also registered as the default "mock" model in production).
	callCount atomic.Int64
}

// NewMock creates a mock provider that always returns the given response.
func NewMock(response string) *Mock {
	return &Mock{response: response}
}

// NewMockWithToolCall creates a mock that returns a tool call on first request,
// then a text response on subsequent requests.
func NewMockWithToolCall(toolName string, input map[string]string) *Mock {
	return &Mock{
		response: "Tool result processed.",
		toolCall: &agent.ToolCall{
			ID:    "call_mock_001",
			Name:  toolName,
			Input: input,
		},
	}
}

func (m *Mock) Name() string { return "mock" }

func (m *Mock) Chat(ctx context.Context, messages []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	n := m.callCount.Add(1)

	// If we have a tool call and this is the first request, return tool call
	if m.toolCall != nil && n == 1 {
		return agent.Message{
			Role:      agent.RoleAssistant,
			ToolCalls: []agent.ToolCall{*m.toolCall},
		}, nil
	}

	return agent.Message{
		Role:    agent.RoleAssistant,
		Content: m.response,
	}, nil
}
