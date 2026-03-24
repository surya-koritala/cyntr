package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel/log"
)

var logger = log.Default().WithModule("mcp")

type Client struct {
	config      ServerConfig
	transport   Transport
	codec       *Codec
	tools       []MCPToolDef
	initialized bool
}

func NewClient(config ServerConfig) *Client {
	return &Client{config: config, codec: NewCodec()}
}

func (c *Client) Connect(ctx context.Context) error {
	var err error
	switch c.config.Transport {
	case "stdio":
		c.transport, err = NewStdioTransport(c.config.Command, c.config.Args, c.config.Env)
	case "http":
		c.transport = NewHTTPTransport(c.config.URL)
	default:
		return fmt.Errorf("unsupported transport: %s", c.config.Transport)
	}
	if err != nil {
		return fmt.Errorf("create transport: %w", err)
	}

	// Initialize handshake
	initParams := InitializeParams{
		ProtocolVersion: "2024-11-05",
		Capabilities:    map[string]any{},
	}
	initParams.ClientInfo.Name = "cyntr"
	initParams.ClientInfo.Version = "0.9.0"

	reqData, id := c.codec.EncodeRequest("initialize", initParams)
	respData, err := c.transport.Send(ctx, reqData)
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	resp, err := c.codec.DecodeResponse(respData)
	if err != nil {
		return fmt.Errorf("decode initialize response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s", resp.Error.Message)
	}
	_ = id

	// Send initialized notification (for stdio)
	if c.config.Transport == "stdio" {
		notif, _ := c.codec.EncodeRequest("notifications/initialized", nil)
		c.transport.Send(ctx, notif)
	}

	c.initialized = true

	// Discover tools
	tools, err := c.DiscoverTools(ctx)
	if err != nil {
		logger.Warn("tool discovery failed", map[string]any{"server": c.config.Name, "error": err.Error()})
	} else {
		c.tools = tools
	}

	logger.Info("MCP server connected", map[string]any{"server": c.config.Name, "tools": len(c.tools)})
	return nil
}

func (c *Client) DiscoverTools(ctx context.Context) ([]MCPToolDef, error) {
	reqData, _ := c.codec.EncodeRequest("tools/list", map[string]any{})
	respData, err := c.transport.Send(ctx, reqData)
	if err != nil {
		return nil, err
	}

	resp, err := c.codec.DecodeResponse(respData)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("tools/list error: %s", resp.Error.Message)
	}

	// Parse tools from result
	resultJSON, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, err
	}

	var toolList struct {
		Tools []MCPToolDef `json:"tools"`
	}
	if err := json.Unmarshal(resultJSON, &toolList); err != nil {
		return nil, fmt.Errorf("parse tools: %w", err)
	}

	return toolList.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*ToolCallResult, error) {
	params := ToolCallParams{Name: name, Arguments: arguments}
	reqData, _ := c.codec.EncodeRequest("tools/call", params)

	respData, err := c.transport.Send(ctx, reqData)
	if err != nil {
		return nil, err
	}

	resp, err := c.codec.DecodeResponse(respData)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("tools/call error: %s", resp.Error.Message)
	}

	resultJSON, _ := json.Marshal(resp.Result)
	var result ToolCallResult
	json.Unmarshal(resultJSON, &result)
	return &result, nil
}

func (c *Client) Tools() []MCPToolDef { return c.tools }
func (c *Client) IsConnected() bool    { return c.initialized }

func (c *Client) Close() error {
	if c.transport != nil {
		return c.transport.Close()
	}
	return nil
}
