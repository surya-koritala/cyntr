package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// MCPToolAdapter wraps a remote MCP tool as a native Cyntr agent.Tool.
type MCPToolAdapter struct {
	client     *Client
	serverName string
	toolDef    MCPToolDef
	params     map[string]agent.ToolParam
}

func NewMCPToolAdapter(client *Client, serverName string, def MCPToolDef) *MCPToolAdapter {
	params := convertInputSchema(def.InputSchema)
	return &MCPToolAdapter{
		client:     client,
		serverName: serverName,
		toolDef:    def,
		params:     params,
	}
}

func (a *MCPToolAdapter) Name() string {
	return "mcp_" + a.serverName + "_" + a.toolDef.Name
}

func (a *MCPToolAdapter) Description() string {
	desc := a.toolDef.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool from %s server", a.serverName)
	}
	return fmt.Sprintf("[MCP:%s] %s", a.serverName, desc)
}

func (a *MCPToolAdapter) Parameters() map[string]agent.ToolParam {
	return a.params
}

func (a *MCPToolAdapter) Execute(ctx context.Context, input map[string]string) (string, error) {
	// Convert map[string]string to map[string]any
	args := make(map[string]any, len(input))
	for k, v := range input {
		args[k] = v
	}

	result, err := a.client.CallTool(ctx, a.toolDef.Name, args)
	if err != nil {
		return "", fmt.Errorf("MCP tool %s: %w", a.Name(), err)
	}

	// Concatenate text content blocks
	var output strings.Builder
	for _, block := range result.Content {
		if block.Type == "text" {
			output.WriteString(block.Text)
		}
	}

	text := output.String()
	if len(text) > 65536 {
		text = text[:65536] + "\n... (truncated)"
	}

	if result.IsError {
		return text, fmt.Errorf("MCP tool error: %s", text)
	}
	return text, nil
}

// convertInputSchema converts MCP JSON Schema to Cyntr ToolParam map.
func convertInputSchema(schema map[string]any) map[string]agent.ToolParam {
	params := make(map[string]agent.ToolParam)

	props, _ := schema["properties"].(map[string]any)
	requiredList, _ := schema["required"].([]any)
	required := make(map[string]bool)
	for _, r := range requiredList {
		if s, ok := r.(string); ok {
			required[s] = true
		}
	}

	for name, prop := range props {
		p, ok := prop.(map[string]any)
		if !ok {
			continue
		}
		tp := agent.ToolParam{
			Type:        fmt.Sprintf("%v", p["type"]),
			Description: fmt.Sprintf("%v", p["description"]),
			Required:    required[name],
		}
		if tp.Type == "<nil>" {
			tp.Type = "string"
		}
		if tp.Description == "<nil>" {
			tp.Description = ""
		}
		params[name] = tp
	}

	return params
}
