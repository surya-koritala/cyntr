package channel

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func newPairingStore(t *testing.T) *PairingStore {
	t.Helper()
	s, err := NewPairingStore(filepath.Join(t.TempDir(), "pairing.db"))
	if err != nil {
		t.Fatalf("NewPairingStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPairingStoreApproveByCode(t *testing.T) {
	s := newPairingStore(t)
	if s.IsPaired("acme", "slack", "U1") {
		t.Fatal("unknown sender should not be paired")
	}
	code, err := s.IssueCode("acme", "slack", "U1")
	if err != nil || code == "" {
		t.Fatalf("issue: %q %v", code, err)
	}
	user, err := s.ApproveCode("acme", "slack", code)
	if err != nil || user != "U1" {
		t.Fatalf("approve: %q %v", user, err)
	}
	if !s.IsPaired("acme", "slack", "U1") {
		t.Fatal("sender should be paired after approval")
	}
	if pending, _ := s.ListPending("acme"); len(pending) != 0 {
		t.Fatalf("approved request should clear pending, got %d", len(pending))
	}
}

func TestPairingTenantIsolation(t *testing.T) {
	s := newPairingStore(t)
	s.ApproveUser("acme", "slack", "U1")
	if s.IsPaired("globex", "slack", "U1") {
		t.Fatal("approval in one tenant must not pair the same user in another")
	}
	if !s.IsPaired("acme", "slack", "U1") {
		t.Fatal("acme/U1 should be paired")
	}
}

func TestGatePairingIssuesCodeForUnknown(t *testing.T) {
	s := newPairingStore(t)
	g := NewGate(s, DMPairing)
	msg := InboundMessage{Channel: "slack", Tenant: "acme", UserID: "U1", Text: "hi", Agent: "a"}

	ok, reply := g.Check(msg)
	if ok {
		t.Fatal("unknown sender under pairing should be blocked")
	}
	if !strings.Contains(reply, "code") {
		t.Fatalf("reply should contain a pairing code: %q", reply)
	}
	// After approval the same sender is allowed through.
	s.ApproveUser("acme", "slack", "U1")
	if ok, _ := g.Check(msg); !ok {
		t.Fatal("approved sender should be allowed")
	}
}

func TestGateOpenAndClosed(t *testing.T) {
	g := NewGate(nil, DMOpen)
	if ok, _ := g.Check(InboundMessage{Channel: "web", UserID: "anyone"}); !ok {
		t.Fatal("open policy with empty allowlist should allow everyone")
	}
	g.SetPolicy("web", DMClosed, nil)
	if ok, _ := g.Check(InboundMessage{Channel: "web", UserID: "anyone"}); ok {
		t.Fatal("closed policy should block")
	}
	g.SetPolicy("web", DMOpen, []string{"alice"})
	if ok, _ := g.Check(InboundMessage{Channel: "web", UserID: "bob"}); ok {
		t.Fatal("open with an allowlist should reject non-listed users")
	}
	if ok, _ := g.Check(InboundMessage{Channel: "web", UserID: "alice"}); !ok {
		t.Fatal("allowlisted user should pass")
	}
}

// An unknown sender must never reach the agent through the manager.
func TestManagerGateBlocksAgentForUnknown(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	var agentCalled bool
	bus.Handle("agent_runtime", "agent.chat", func(msg ipc.Message) (ipc.Message, error) {
		agentCalled = true
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: agent.ChatResponse{Content: "should not happen"}}, nil
	})

	s := newPairingStore(t)
	m := &Manager{adapters: map[string]ChannelAdapter{}, bus: bus, gate: NewGate(s, DMPairing)}

	reply, err := m.routeInbound(InboundMessage{Channel: "slack", Tenant: "acme", UserID: "U9", Text: "hello", Agent: "a"})
	if err != nil {
		t.Fatalf("routeInbound: %v", err)
	}
	if agentCalled {
		t.Fatal("agent must NOT be invoked for an unpaired sender")
	}
	if !strings.Contains(reply, "code") {
		t.Fatalf("unpaired sender should get a pairing code, got %q", reply)
	}

	// Approve, then the next message reaches the agent.
	s.ApproveUser("acme", "slack", "U9")
	if _, err := m.routeInbound(InboundMessage{Channel: "slack", Tenant: "acme", UserID: "U9", Text: "hello again", Agent: "a"}); err != nil {
		t.Fatalf("routeInbound after approval: %v", err)
	}
	if !agentCalled {
		t.Fatal("approved sender's message should reach the agent")
	}
}
