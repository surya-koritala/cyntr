package agent

import (
	"fmt"
	"time"
)

// TopicTurnCompleted is the IPC topic on which the runtime broadcasts a
// TurnRecord after every completed chat turn. Consumers subscribe with
// bus.Subscribe("<module>", TopicTurnCompleted, handler); delivery is
// fire-and-forget and fans out to all subscribers.
const TopicTurnCompleted = "agent.turn_completed"

// TurnRecord is the payload published on TopicTurnCompleted once a chat turn
// reaches its terminal (no more tool calls) state. It is an in-process,
// tenant-scoped snapshot intended for asynchronous consumers such as the
// learning loop, cross-session recall indexer, and trajectory capture.
//
// Privacy: UserMessage and Response carry RAW, pre-sanitization text. Any
// consumer that persists or displays them MUST first run them through
// MaskSecrets/RedactPII, exactly as the chat response path does.
type TurnRecord struct {
	Tenant       string    `json:"tenant"`
	User         string    `json:"user"`
	Session      string    `json:"session"`
	Agent        string    `json:"agent"`
	Model        string    `json:"model"`
	UserMessage  string    `json:"user_message"`  // raw message that started the turn
	Response     string    `json:"response"`      // raw final assistant content
	ToolsUsed    []string  `json:"tools_used"`    // distinct tool names invoked
	ToolCalls    int       `json:"tool_calls"`    // total tool invocations across the loop
	Turns        int       `json:"turns"`         // model turns taken to reach completion
	InputTokens  int       `json:"input_tokens"`  // summed across all model calls
	OutputTokens int       `json:"output_tokens"` // summed across all model calls
	TotalTokens  int       `json:"total_tokens"`
	Outcome      string    `json:"outcome"` // "ok" (extend with error outcomes later)
	DurationMS   int64     `json:"duration_ms"`
	StartedAt    time.Time `json:"started_at"`
	// Subagent is true when this turn was spawned by another agent
	// (orchestrate/delegate). Consumers that write into the shared user's
	// durable state (recall, learning loop) skip subagent turns so child
	// work doesn't contaminate the parent/user.
	Subagent bool `json:"subagent"`
}

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
	Role         Role
	Content      string
	ToolCalls    []ToolCall   // set when assistant requests tool use
	ToolResults  []ToolResult // set when providing tool results
	Attachments  []Attachment // optional media attachments (multimodal)
	InputTokens  int          // token count from provider (input)
	OutputTokens int          // token count from provider (output)
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
	MaxTurns            int               `yaml:"max_turns" json:"max_turns"`                         // max tool-use turns per request (default 10)
	MaxHistory          int               `yaml:"max_history" json:"max_history"`                     // sliding window: max messages to keep in history (0 = unlimited)
	SummarizeThreshold  int               `yaml:"summarize_threshold" json:"summarize_threshold"`     // auto-compact history when exceeding this count (0 = disabled)
	Secrets             map[string]string `yaml:"secrets" json:"secrets"`                             // per-agent env vars / credentials
	RateLimit           int               `yaml:"rate_limit" json:"rate_limit"`                       // max requests per minute (0 = unlimited)
	Skills              []string          `yaml:"skills" json:"skills"`
	MCPServers          []string          `yaml:"mcp_servers" json:"mcp_servers"`
	AutoMemory          bool              `yaml:"auto_memory" json:"auto_memory"`
	Sandbox             SandboxConfig     `yaml:"sandbox" json:"sandbox"` // per-session sandboxing (C15)
}

// SandboxConfig controls per-session sandboxing of the tool surface (C15).
type SandboxConfig struct {
	// Mode is "off" (default), "non-main" (sandbox every agent except the one
	// named "main"), or "always".
	Mode string `yaml:"mode" json:"mode"`
	// Backend is "" / "process" (host) or "docker". Host code-execution tools
	// (shell_exec, code_interpreter) are stripped from a sandboxed session
	// unless Backend is "docker", i.e. they run containerized.
	Backend string `yaml:"backend" json:"backend"`
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
