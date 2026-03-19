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

// StreamChunk represents a piece of a streaming response.
type StreamChunk struct {
	Type      string    // "text", "tool_use_start", "tool_use_input", "done", "error"
	Text      string    // text content (for "text" chunks)
	ToolCall  *ToolCall // set on "tool_use_start"
	ToolInput string    // partial JSON input (for "tool_use_input")
	Error     error     // set on "error" chunks
}

// StreamingProvider extends ModelProvider with streaming support.
// Providers that support streaming implement this in addition to ModelProvider.
type StreamingProvider interface {
	ModelProvider
	// ChatStream sends messages and returns a channel of streaming chunks.
	// The channel is closed when the response is complete.
	// The caller must drain the channel.
	ChatStream(ctx context.Context, messages []Message, tools []ToolDef) (<-chan StreamChunk, error)
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
