package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

type Manager struct {
	bus     *ipc.Bus
	toolReg *agent.ToolRegistry
	clients map[string]*Client
	configs []ServerConfig
	mu      sync.RWMutex
}

func NewManager(toolReg *agent.ToolRegistry) *Manager {
	return &Manager{
		toolReg: toolReg,
		clients: make(map[string]*Client),
	}
}

func (m *Manager) SetConfigs(configs []ServerConfig) { m.configs = configs }

func (m *Manager) Name() string           { return "mcp" }
func (m *Manager) Dependencies() []string { return []string{"agent_runtime"} }

func (m *Manager) Init(ctx context.Context, svc *kernel.Services) error {
	m.bus = svc.Bus
	return nil
}

func (m *Manager) Start(ctx context.Context) error {
	m.bus.Handle("mcp", "mcp.server.add", m.handleAddServer)
	m.bus.Handle("mcp", "mcp.server.remove", m.handleRemoveServer)
	m.bus.Handle("mcp", "mcp.server.list", m.handleListServers)
	m.bus.Handle("mcp", "mcp.server.tools", m.handleServerTools)
	m.bus.Handle("mcp", "mcp.marketplace.search", m.handleMarketplaceSearch)

	// Connect to configured servers
	for _, cfg := range m.configs {
		if err := m.connectServer(ctx, cfg); err != nil {
			logger.Warn("MCP server connection failed", map[string]any{
				"server": cfg.Name, "error": err.Error(),
			})
		}
	}
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, client := range m.clients {
		if err := client.Close(); err != nil {
			logger.Warn("MCP server close error", map[string]any{"server": name, "error": err.Error()})
		}
	}
	return nil
}

func (m *Manager) Health(ctx context.Context) kernel.HealthStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	connected := 0
	totalTools := 0
	for _, c := range m.clients {
		if c.IsConnected() {
			connected++
			totalTools += len(c.Tools())
		}
	}
	return kernel.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("%d MCP servers, %d tools", connected, totalTools),
	}
}

func (m *Manager) connectServer(ctx context.Context, cfg ServerConfig) error {
	client := NewClient(cfg)
	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := client.Connect(connectCtx); err != nil {
		return err
	}

	m.mu.Lock()
	m.clients[cfg.Name] = client
	m.mu.Unlock()

	// Register discovered tools
	for _, tool := range client.Tools() {
		adapter := NewMCPToolAdapter(client, cfg.Name, tool)
		m.toolReg.Register(adapter)
	}

	return nil
}

func (m *Manager) handleAddServer(msg ipc.Message) (ipc.Message, error) {
	cfg, ok := msg.Payload.(ServerConfig)
	if !ok {
		// Try map
		if data, ok := msg.Payload.(map[string]any); ok {
			cfg = ServerConfig{
				Name:      fmt.Sprintf("%v", data["name"]),
				Command:   fmt.Sprintf("%v", data["command"]),
				Transport: fmt.Sprintf("%v", data["transport"]),
				URL:       fmt.Sprintf("%v", data["url"]),
			}
			if args, ok := data["args"].([]any); ok {
				for _, a := range args {
					cfg.Args = append(cfg.Args, fmt.Sprintf("%v", a))
				}
			}
		} else {
			return ipc.Message{}, fmt.Errorf("expected ServerConfig, got %T", msg.Payload)
		}
	}

	if err := m.connectServer(context.Background(), cfg); err != nil {
		return ipc.Message{}, err
	}

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "connected"}, nil
}

func (m *Manager) handleRemoveServer(msg ipc.Message) (ipc.Message, error) {
	name, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string, got %T", msg.Payload)
	}

	m.mu.Lock()
	client, exists := m.clients[name]
	if exists {
		// Unregister tools
		for _, tool := range client.Tools() {
			toolName := "mcp_" + name + "_" + tool.Name
			m.toolReg.Unregister(toolName)
		}
		client.Close()
		delete(m.clients, name)
	}
	m.mu.Unlock()

	if !exists {
		return ipc.Message{}, fmt.Errorf("MCP server %q not found", name)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "disconnected"}, nil
}

func (m *Manager) handleListServers(msg ipc.Message) (ipc.Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var statuses []ServerStatus
	for name, client := range m.clients {
		status := "connected"
		if !client.IsConnected() {
			status = "disconnected"
		}
		var toolNames []string
		for _, t := range client.Tools() {
			toolNames = append(toolNames, t.Name)
		}
		statuses = append(statuses, ServerStatus{
			Name:      name,
			Transport: client.config.Transport,
			Status:    status,
			ToolCount: len(client.Tools()),
			Tools:     toolNames,
		})
	}
	if statuses == nil {
		statuses = []ServerStatus{}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: statuses}, nil
}

func (m *Manager) handleServerTools(msg ipc.Message) (ipc.Message, error) {
	name, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string, got %T", msg.Payload)
	}

	m.mu.RLock()
	client, exists := m.clients[name]
	m.mu.RUnlock()

	if !exists {
		return ipc.Message{}, fmt.Errorf("MCP server %q not found", name)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: client.Tools()}, nil
}

func (m *Manager) handleMarketplaceSearch(msg ipc.Message) (ipc.Message, error) {
	query, _ := msg.Payload.(string)
	results := SearchBuiltinMCPCatalog(query)
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: results}, nil
}
