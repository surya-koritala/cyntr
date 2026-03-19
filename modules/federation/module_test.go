package federation

import (
	"context"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func TestModuleImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Module)(nil)
}

func TestModuleAddPeerViaIPC(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	mod := NewModule("local-node")
	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "federation", Topic: "federation.join",
		Payload: Peer{Name: "east-node", Endpoint: "http://east.corp.com:8443"},
	})
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if resp.Payload != "ok" {
		t.Fatalf("expected ok, got %v", resp.Payload)
	}
}

func TestModuleListPeersViaIPC(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	mod := NewModule("local-node")
	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "federation", Topic: "federation.join",
		Payload: Peer{Name: "east", Endpoint: "http://east"},
	})
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "federation", Topic: "federation.join",
		Payload: Peer{Name: "west", Endpoint: "http://west"},
	})

	resp, err := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "federation", Topic: "federation.peers",
	})
	if err != nil {
		t.Fatalf("peers: %v", err)
	}

	peers, ok := resp.Payload.([]Peer)
	if !ok {
		t.Fatalf("expected []Peer, got %T", resp.Payload)
	}
	if len(peers) != 2 {
		t.Fatalf("expected 2, got %d", len(peers))
	}
}

func TestModuleRemovePeerViaIPC(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	mod := NewModule("local-node")
	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "federation", Topic: "federation.join",
		Payload: Peer{Name: "temp", Endpoint: "http://temp"},
	})

	resp, _ := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "federation", Topic: "federation.remove",
		Payload: "temp",
	})
	if resp.Payload != "ok" {
		t.Fatalf("expected ok, got %v", resp.Payload)
	}
}

func TestModuleHealthy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	mod := NewModule("local")
	ctx := context.Background()
	mod.Init(ctx, &kernel.Services{Bus: bus})
	mod.Start(ctx)
	defer mod.Stop(ctx)
	h := mod.Health(ctx)
	if !h.Healthy {
		t.Fatalf("expected healthy: %s", h.Message)
	}
}
