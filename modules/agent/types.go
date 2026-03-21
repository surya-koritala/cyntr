package agent

import "fmt"

// Role identifies the sender of a message in a conversation.
type Role int

const (
	RoleUser      Role = iota
	RoleAssistant
	RoleSystem
	RoleTool
)

func (r Role) String() string {
	switch r {
	case RoleUser:
		return "user"
	case RoleAssistant:
		return "assistant"
	case RoleSystem:
		return "system"
	case RoleTool:
		return "tool"
	default:
		return fmt.Sprintf("unknown(%d)", int(r))
	}
}

// Attachment represents a media attachment for multimodal messages.
type Attachment struct {
	Type     string // "image", "file", "audio"
	URL      string // URL or base64 data
	MimeType string // "image/png", etc.
	Name     string // filename
}

// Message represents a single message in a conversation.
type Message struct {
	Role        Role
	Content     string
	ToolCalls   []ToolCall   // set when assistant requests tool use
	ToolResults []ToolResult // set when providing tool results
	Attachments []Attachment // optional media attachments (multimodal)
}

// ToolCall represents a request from the model to execute a tool.
type ToolCall struct {
	ID    string
	Name  string
	Input map[string]string
}

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}

// AgentConfig defines the configuration for a single agent instance.
type AgentConfig struct {
	Name         string            `yaml:"name" json:"name"`
	Tenant       string            `yaml:"tenant" json:"tenant"`
	Model        string            `yaml:"model" json:"model"`                 // provider name: "claude", "gpt", "mock"
	SystemPrompt string            `yaml:"system_prompt" json:"system_prompt"`
	Tools        []string          `yaml:"tools" json:"tools"`                 // allowed tool names
	MaxTurns     int               `yaml:"max_turns" json:"max_turns"`         // max tool-use turns per request (default 10)
	Secrets      map[string]string `yaml:"secrets" json:"secrets"`             // per-agent env vars / credentials
}

// ProgressEvent is published during tool execution to inform channels of agent activity.
type ProgressEvent struct {
	Agent     string `json:"agent"`
	Tenant    string `json:"tenant"`
	Channel   string `json:"channel"`    // adapter name: "slack", "teams"
	ChannelID string `json:"channel_id"` // platform channel ID
	ToolName  string `json:"tool_name"`
	Status    string `json:"status"`  // "running", "complete", "error"
	Message   string `json:"message"`
}

// ActivityEvent represents a real-time agent activity for the dashboard log stream.
type ActivityEvent struct {
	Timestamp string `json:"timestamp"`
	Agent     string `json:"agent"`
	Tenant    string `json:"tenant"`
	Type      string `json:"type"`   // "chat_start", "tool_exec", "tool_result", "chat_complete", "error"
	Detail    string `json:"detail"`
}

// ChatRequest is the IPC payload for agent.chat requests.
type ChatRequest struct {
	Agent     string // agent name
	Tenant    string // tenant name
	User      string // user making the request
	Message   string // user's message
	Channel   string // channel adapter name (for progress routing)
	ChannelID string // channel-specific ID (for progress routing)
}

// ChatResponse is the IPC payload for agent.chat responses.
type ChatResponse struct {
	Agent     string
	Content   string
	ToolsUsed []string // names of tools that were called
}
