package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Gateway is the Proxy Gateway kernel module.
type Gateway struct {
	listenAddr     string
	upstreamURL    string
	identitySecret string
	bus            *ipc.Bus
	server         *http.Server
	listener       net.Listener
	mu             sync.RWMutex
	agents         map[string]ExternalAgent
}

// NewGateway creates a new Proxy Gateway module.
func NewGateway(listenAddr string) *Gateway {
	return &Gateway{
		listenAddr: listenAddr,
		agents:     make(map[string]ExternalAgent),
	}
}

// SetUpstreamURL sets the default upstream URL for proxied requests.
func (g *Gateway) SetUpstreamURL(url string) {
	g.upstreamURL = url
}

// SetIdentitySecret sets the shared HMAC secret used to authenticate the
// caller-supplied tenant/user identity for both policy decisions and rate
// limiting. When unset (the default), the gateway falls back to trusting the
// X-Cyntr-Tenant/X-Cyntr-User headers (only safe behind a trusted
// authenticating proxy). Wire this from configuration / a secret such as the
// CYNTR_PROXY_IDENTITY_SECRET environment variable.
func (g *Gateway) SetIdentitySecret(secret string) {
	g.identitySecret = secret
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

	// Create HTTP handler with upstream URL for proxying
	handler := NewHandler(g.bus, g.upstreamURL)
	handler.SetIdentitySecret(g.identitySecret)

	// Rate limiting: 100 requests per minute per tenant
	rateLimiter := NewRateLimiter(100, 1*time.Minute)
	rateLimiter.SetIdentitySecret(g.identitySecret)
	wrappedHandler := rateLimiter.Middleware(handler)

	// Start HTTP server
	ln, err := net.Listen("tcp", g.listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	g.listener = ln

	// Set timeouts so a slow or stalled client cannot hold a connection (and
	// its goroutine) open indefinitely (Slowloris-style resource exhaustion).
	g.server = &http.Server{
		Handler:           wrappedHandler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

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

	// Validate the caller-supplied tenant. An agent stored under an empty or
	// whitespace-only tenant would either leak across the tenant boundary or
	// be unreachable by any tenant-scoped list. Fail closed.
	if strings.TrimSpace(agent.Tenant) == "" {
		return ipc.Message{}, fmt.Errorf("external agent registration requires a non-empty tenant")
	}
	if strings.TrimSpace(agent.Name) == "" {
		return ipc.Message{}, fmt.Errorf("external agent registration requires a non-empty name")
	}

	g.mu.Lock()
	g.agents[agent.Key()] = agent
	g.mu.Unlock()

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

// handleList returns the external agents registered for a single tenant. The
// payload MUST be the caller's tenant string (mirroring agent_runtime's
// agent.list). The result is scoped to that tenant so one tenant cannot
// enumerate another tenant's registered agents. An empty or non-string payload
// fails closed.
func (g *Gateway) handleList(msg ipc.Message) (ipc.Message, error) {
	tenantFilter, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected tenant string, got %T", msg.Payload)
	}
	if strings.TrimSpace(tenantFilter) == "" {
		return ipc.Message{}, fmt.Errorf("proxy.list requires a non-empty tenant")
	}

	g.mu.RLock()
	agents := make([]ExternalAgent, 0)
	for _, a := range g.agents {
		if a.Tenant == tenantFilter {
			agents = append(agents, a)
		}
	}
	g.mu.RUnlock()

	sort.Slice(agents, func(i, j int) bool {
		return agents[i].Key() < agents[j].Key()
	})

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: agents}, nil
}
