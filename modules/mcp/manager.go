package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/netguard"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

type Manager struct {
	bus     *ipc.Bus
	toolReg *agent.ToolRegistry
	clients map[string]*Client
	configs []ServerConfig
	mu      sync.RWMutex
}

// clientKey scopes an MCP client by the owning tenant so one tenant cannot
// enumerate, address, or remove another tenant's server (which may share the
// same human-chosen Name). The NUL separator cannot appear in a tenant id or
// server name, so the composite key is unambiguous.
func clientKey(tenant, name string) string { return tenant + "\x00" + name }

// tenantOf extracts the owning tenant for a server config. Configured servers
// (loaded from disk at startup) and callers that do not specify a tenant fall
// back to the shared "system" scope rather than an empty, spoofable key.
func tenantOf(cfg ServerConfig) string {
	if cfg.Scope != "" {
		return cfg.Scope
	}
	return "system"
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
	for key, client := range m.clients {
		if err := client.Close(); err != nil {
			logger.Warn("MCP server close error", map[string]any{"server": key, "error": err.Error()})
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

// allowedStdioCommands is the allowlist of executables an MCP stdio server may
// launch. Every BuiltinMCPCatalog entry uses "npx"; anything else is rejected
// so a caller of mcp.server.add cannot spawn an arbitrary binary (RCE).
var allowedStdioCommands = map[string]bool{"npx": true}

func (m *Manager) connectServer(ctx context.Context, cfg ServerConfig) error {
	// stdio servers spawn a subprocess from cfg.Command — gate it on an
	// allowlist. http/sse transports do not exec, so they skip this check.
	if cfg.Transport == "" || cfg.Transport == "stdio" {
		base := cfg.Command
		if i := strings.LastIndexAny(base, "/\\"); i >= 0 {
			base = base[i+1:]
		}
		if !allowedStdioCommands[base] {
			return fmt.Errorf("mcp: command %q is not allowed for stdio servers (allowed: npx)", cfg.Command)
		}
	}

	// http/sse transports fetch a caller-supplied URL server-side — run it
	// through the shared SSRF guard so it cannot target loopback, link-local
	// (cloud metadata), or private addresses.
	if cfg.Transport == "http" || cfg.Transport == "sse" {
		if err := netguard.ValidatePublicURL(cfg.URL); err != nil {
			return fmt.Errorf("mcp: http server URL rejected: %w", err)
		}
	}

	client := NewClient(cfg)
	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := client.Connect(connectCtx); err != nil {
		return err
	}

	key := clientKey(tenantOf(cfg), cfg.Name)

	m.mu.Lock()
	// Closing an existing client for the same (tenant, name) leaks its
	// subprocess/HTTP connection if we just overwrite it — close it first.
	if prev, ok := m.clients[key]; ok {
		for _, tool := range prev.Tools() {
			m.toolReg.Unregister("mcp_" + cfg.Name + "_" + tool.Name)
		}
		if err := prev.Close(); err != nil {
			logger.Warn("MCP server close error on reconnect", map[string]any{"server": cfg.Name, "error": err.Error()})
		}
	}
	m.clients[key] = client
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
			if t, ok := data["tenant"].(string); ok {
				cfg.Scope = t
			} else if t, ok := data["scope"].(string); ok {
				cfg.Scope = t
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
	tenant, name, err := serverRef(msg.Payload)
	if err != nil {
		return ipc.Message{}, err
	}
	key := clientKey(tenant, name)

	m.mu.Lock()
	client, exists := m.clients[key]
	if exists {
		// Unregister tools
		for _, tool := range client.Tools() {
			toolName := "mcp_" + name + "_" + tool.Name
			m.toolReg.Unregister(toolName)
		}
		client.Close()
		delete(m.clients, key)
	}
	m.mu.Unlock()

	if !exists {
		return ipc.Message{}, fmt.Errorf("MCP server %q not found", name)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "disconnected"}, nil
}

// serverRef resolves the (tenant, name) a request addresses. Accepts a
// {"tenant","name"} map (the scoped form) or a bare name string (legacy,
// scoped to the shared "system" tenant) so a caller cannot reach another
// tenant's server by guessing its name.
func serverRef(payload any) (tenant, name string, err error) {
	switch p := payload.(type) {
	case map[string]any:
		name, _ = p["name"].(string)
		if t, ok := p["tenant"].(string); ok {
			tenant = t
		} else if t, ok := p["scope"].(string); ok {
			tenant = t
		}
	case map[string]string:
		name = p["name"]
		if p["tenant"] != "" {
			tenant = p["tenant"]
		} else {
			tenant = p["scope"]
		}
	case string:
		name = p
	default:
		return "", "", fmt.Errorf("expected server reference, got %T", payload)
	}
	if name == "" {
		return "", "", fmt.Errorf("missing server name")
	}
	if tenant == "" {
		tenant = "system"
	}
	return tenant, name, nil
}

func (m *Manager) handleListServers(msg ipc.Message) (ipc.Message, error) {
	// Scope the listing to the requesting tenant so tenants cannot enumerate
	// each other's servers. A bare/empty payload lists the shared "system"
	// scope only.
	tenant := "system"
	switch p := msg.Payload.(type) {
	case map[string]any:
		if t, ok := p["tenant"].(string); ok && t != "" {
			tenant = t
		} else if t, ok := p["scope"].(string); ok && t != "" {
			tenant = t
		}
	case map[string]string:
		if p["tenant"] != "" {
			tenant = p["tenant"]
		} else if p["scope"] != "" {
			tenant = p["scope"]
		}
	case string:
		if p != "" {
			tenant = p
		}
	}
	prefix := tenant + "\x00"

	m.mu.RLock()
	defer m.mu.RUnlock()

	var statuses []ServerStatus
	for key, client := range m.clients {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		name := key[len(prefix):]
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
	tenant, name, err := serverRef(msg.Payload)
	if err != nil {
		return ipc.Message{}, err
	}

	m.mu.RLock()
	client, exists := m.clients[clientKey(tenant, name)]
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
