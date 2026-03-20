package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

func TestGatewayImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Gateway)(nil)
}

func TestGatewayStartsHTTPServer(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	setupPolicyHandler(bus, policy.Allow)

	gw := NewGateway("127.0.0.1:0") // port 0 = random available port
	ctx := context.Background()
	gw.Init(ctx, &kernel.Services{Bus: bus})
	gw.Start(ctx)
	defer gw.Stop(ctx)

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	addr := gw.Addr()
	if addr == "" {
		t.Fatal("expected server address")
	}

	// Hit health endpoint
	resp, err := http.Get("http://" + addr + "/health")
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestGatewayRegisterExternalAgent(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	gw := NewGateway("127.0.0.1:0")
	ctx := context.Background()
	gw.Init(ctx, &kernel.Services{Bus: bus})
	gw.Start(ctx)
	defer gw.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "proxy", Topic: "proxy.register",
		Payload: ExternalAgent{
			Name: "marketing-openclaw", Tenant: "marketing",
			Type: "openclaw", Endpoint: "http://localhost:18789",
		},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("expected ok, got %v", resp.Payload)
	}
}

func TestGatewayListExternalAgents(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	gw := NewGateway("127.0.0.1:0")
	ctx := context.Background()
	gw.Init(ctx, &kernel.Services{Bus: bus})
	gw.Start(ctx)
	defer gw.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Register an agent
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "proxy", Topic: "proxy.register",
		Payload: ExternalAgent{Name: "test-agent", Tenant: "finance", Type: "openclaw", Endpoint: "http://localhost:1234"},
	})

	// List
	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "proxy", Topic: "proxy.list",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	agents, ok := resp.Payload.([]ExternalAgent)
	if !ok {
		t.Fatalf("expected []ExternalAgent, got %T", resp.Payload)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1, got %d", len(agents))
	}
}

func TestGatewayHealthy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	gw := NewGateway("127.0.0.1:0")
	ctx := context.Background()
	gw.Init(ctx, &kernel.Services{Bus: bus})
	gw.Start(ctx)
	defer gw.Stop(ctx)

	time.Sleep(100 * time.Millisecond)
	health := gw.Health(ctx)
	if !health.Healthy {
		t.Fatalf("expected healthy: %s", health.Message)
	}
}

func TestGatewayDeniesWithoutPolicy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	// No policy handler — fail-closed

	gw := NewGateway("127.0.0.1:0")
	ctx := context.Background()
	gw.Init(ctx, &kernel.Services{Bus: bus})
	gw.Start(ctx)
	defer gw.Stop(ctx)

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Post(
		"http://"+gw.Addr()+"/v1/messages",
		"application/json",
		strings.NewReader(`{"model":"claude"}`),
	)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "policy engine unavailable" {
		t.Fatalf("expected policy error, got %v", body)
	}
}
