package mcp

import "time"

// --- JSON-RPC 2.0 Types ---

type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      int64  `json:"id"`
}

type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	Result  any           `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
	ID      int64         `json:"id"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// --- MCP Protocol Types ---

type InitializeParams struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ClientInfo      struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"clientInfo"`
}

type InitializeResult struct {
	ProtocolVersion string `json:"protocolVersion"`
	Capabilities    struct {
		Tools map[string]any `json:"tools,omitempty"`
	} `json:"capabilities"`
	ServerInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"serverInfo"`
}

type MCPToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// --- Configuration ---

type ServerConfig struct {
	Name      string            `yaml:"name" json:"name"`
	Command   string            `yaml:"command" json:"command,omitempty"`
	Args      []string          `yaml:"args" json:"args,omitempty"`
	URL       string            `yaml:"url" json:"url,omitempty"`
	Transport string            `yaml:"transport" json:"transport"`
	Env       map[string]string `yaml:"env" json:"env,omitempty"`
	Scope     string            `yaml:"scope" json:"scope,omitempty"`
}

type ServerStatus struct {
	Name        string    `json:"name"`
	Transport   string    `json:"transport"`
	Status      string    `json:"status"`
	ToolCount   int       `json:"tool_count"`
	Tools       []string  `json:"tools"`
	Error       string    `json:"error,omitempty"`
	ConnectedAt time.Time `json:"connected_at,omitempty"`
}
