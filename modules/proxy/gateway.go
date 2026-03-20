package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Gateway is the Proxy Gateway kernel module.
type Gateway struct {
	listenAddr string
	bus        *ipc.Bus
	server     *http.Server
	listener   net.Listener
	mu         sync.RWMutex
	agents     map[string]ExternalAgent
}

// NewGateway creates a new Proxy Gateway module.
func NewGateway(listenAddr string) *Gateway {
	return &Gateway{
		listenAddr: listenAddr,
		agents:     make(map[string]ExternalAgent),
	}
}

func (g *Gateway) Name() string           { return "proxy" }
func (g *Gateway) Dependencies() []string { return []string{"policy"} }

func (g *Gateway) Init(ctx context.Context, svc *kernel.Services) error {
	g.bus = svc.Bus
	return nil
}

func (g *Gateway) Start(ctx context.Context) error {
	// Register IPC handlers
	g.bus.Handle("proxy", "proxy.register", g.handleRegister)
	g.bus.Handle("proxy", "proxy.list", g.handleList)

	// Create HTTP handler — no default upstream; registered agents serve their own traffic
	handler := NewHandler(g.bus, "")

	// Rate limiting: 100 requests per minute per tenant
	rateLimiter := NewRateLimiter(100, 1*time.Minute)
	wrappedHandler := rateLimiter.Middleware(handler)

	// Start HTTP server
	ln, err := net.Listen("tcp", g.listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	g.listener = ln

	g.server = &http.Server{Handler: wrappedHandler}

	go g.server.Serve(ln) //nolint:errcheck

	return nil
}

func (g *Gateway) Stop(ctx context.Context) error {
	if g.server != nil {
		return g.server.Shutdown(ctx)
	}
	return nil
}

func (g *Gateway) Health(ctx context.Context) kernel.HealthStatus {
	if g.listener == nil {
		return kernel.HealthStatus{Healthy: false, Message: "not listening"}
	}
	return kernel.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("listening on %s", g.listener.Addr().String()),
	}
}

// Addr returns the actual address the server is listening on.
// Useful when listening on port 0 (OS-assigned port).
func (g *Gateway) Addr() string {
	if g.listener == nil {
		return ""
	}
	return g.listener.Addr().String()
}

func (g *Gateway) handleRegister(msg ipc.Message) (ipc.Message, error) {
	agent, ok := msg.Payload.(ExternalAgent)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected ExternalAgent, got %T", msg.Payload)
	}

	g.mu.Lock()
	g.agents[agent.Key()] = agent
	g.mu.Unlock()

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (g *Gateway) handleList(msg ipc.Message) (ipc.Message, error) {
	g.mu.RLock()
	agents := make([]ExternalAgent, 0, len(g.agents))
	for _, a := range g.agents {
		agents = append(agents, a)
	}
	g.mu.RUnlock()

	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Key() < agents[j].Key()
	})

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: agents}, nil
}
