package providers

import (
	"context"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// Mock is a deterministic model provider for testing.
type Mock struct {
	response  string
	toolCall  *agent.ToolCall
	callCount int
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
	m.callCount++

	// If we have a tool call and this is the first request, return tool call
	if m.toolCall != nil && m.callCount == 1 {
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
