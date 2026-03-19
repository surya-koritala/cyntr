package agent

import "context"

// ModelProvider is the interface for LLM backends.
// Each provider (Claude, GPT, Gemini, etc.) implements this.
type ModelProvider interface {
	// Name returns the provider name (e.g., "claude", "gpt", "mock").
	Name() string

	// Chat sends messages to the model and returns a response.
	// tools defines which tools the model can request.
	// The response may contain ToolCalls if the model wants to use tools.
	Chat(ctx context.Context, messages []Message, tools []ToolDef) (Message, error)
}

// ToolDef describes a tool available to the model.
type ToolDef struct {
	Name        string
	Description string
	Parameters  map[string]ToolParam
}

// ToolParam describes a single parameter for a tool.
type ToolParam struct {
	Type        string
	Description string
	Required    bool
}
