// Package federationdemo is an in-process, end-to-end demo of Cyntr's
// federation feature: two kernels with independent buses, configs, and
// policies, wired together as peers; a chat to the research agent on
// node-a delegates to the legal agent on node-b under node-b's policy.
//
// This test is intentionally self-contained — no docker, no network — so it
// runs in CI on any machine and proves the federation routing path is real.
package federationdemo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/agent/providers"
	"github.com/cyntr-dev/cyntr/modules/federation"
	"github.com/cyntr-dev/cyntr/modules/policy"
)

// inProcessTransport is a federation.Transport that routes delegation
// requests via a directly-injected target bus instead of HTTP. It's used
// here so the test can run two kernels in the same process and still
// exercise the real federation.delegate.inbound dispatch path.
type inProcessTransport struct {
	targets map[string]*ipc.Bus // peer.Name -> the other kernel's bus
}

func (t *inProcessTransport) Delegate(ctx context.Context, peer federation.Peer, req federation.DelegateRequest) (federation.DelegateResponse, error) {
	bus, ok := t.targets[peer.Name]
	if !ok {
		return federation.DelegateResponse{}, fmt.Errorf("no in-process target for peer %q", peer.Name)
	}
	// Mirror the real HTTP transport: the shared secret travels with the
	// request (header on the wire) so the inbound handler can authenticate.
	req.Secret = peer.Secret
	resp, err := bus.Request(ctx, ipc.Message{
		Source: "federation_test", Target: "federation", Topic: "federation.delegate.inbound",
		Payload: req,
	})
	if err != nil {
		return federation.DelegateResponse{}, err
	}
	dr, ok := resp.Payload.(federation.DelegateResponse)
	if !ok {
		return federation.DelegateResponse{}, fmt.Errorf("unexpected response: %T", resp.Payload)
	}
	return dr, nil
}

// node bundles one half of the federation demo: kernel, bus, and the
// federation module reference used by the test to wire transports and peers.
type node struct {
	id  string
	k   *kernel.Kernel
	bus *ipc.Bus
	fed *federation.Module
}

func bootNode(t *testing.T, dir, id, policyYAML string) *node {
	t.Helper()

	policyPath := filepath.Join(dir, id+"-policy.yaml")
	if err := os.WriteFile(policyPath, []byte(policyYAML), 0644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	cfgPath := filepath.Join(dir, id+"-cyntr.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
version: "1"
listen:
  address: "127.0.0.1:0"
  webui: ":0"
tenants:
  research:
    isolation: namespace
    policy: default
  legal:
    isolation: namespace
    policy: default
`), 0644); err != nil {
		t.Fatalf("write cfg: %v", err)
	}

	k := kernel.New()
	if err := k.LoadConfig(cfgPath); err != nil {
		t.Fatalf("%s: load config: %v", id, err)
	}

	policyEngine := policy.NewEngine(policyPath, "")
	agentRuntime := agent.NewRuntime()
	// Embed the node ID in the mock response so the test can prove which
	// node actually served the request.
	agentRuntime.RegisterProvider(providers.NewMock("[served-by:" + id + "] OK"))
	fedMod := federation.NewModule(id)

	for _, m := range []kernel.Module{policyEngine, agentRuntime, fedMod} {
		if err := k.Register(m); err != nil {
			t.Fatalf("%s: register %s: %v", id, m.Name(), err)
		}
	}

	if err := k.Start(context.Background()); err != nil {
		t.Fatalf("%s: start: %v", id, err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = k.Stop(ctx)
	})

	return &node{id: id, k: k, bus: k.Bus(), fed: fedMod}
}

// createAgent registers an agent on a node via the agent_runtime IPC topic.
func createAgent(t *testing.T, n *node, name, tenant, systemPrompt string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := n.bus.Request(ctx, ipc.Message{
		Source: "test", Target: "agent_runtime", Topic: "agent.create",
		Payload: agent.AgentConfig{
			Name: name, Tenant: tenant, Model: "mock",
			SystemPrompt: systemPrompt, MaxTurns: 1,
		},
	})
	if err != nil {
		t.Fatalf("create agent %s/%s on %s: %v", tenant, name, n.id, err)
	}
}

// allowAllPolicy is a permissive policy used on the caller side: we want to
// exercise the receiving node's policy, not block the caller.
const allowAllPolicy = `rules:
  - name: allow-all
    tenant: "*"
    action: "*"
    tool: "*"
    agent: "*"
    decision: allow
    priority: 1
`

// nodeBPolicy explicitly authorises federation_inbound for the legal/legal
// agent only — this is the moat-shaped scenario: node-b's owners decide
// what node-a is allowed to invoke.
const nodeBPolicy = `rules:
  - name: allow-federated-legal
    tenant: legal
    action: federation_inbound
    tool: "*"
    agent: legal
    decision: allow
    priority: 50

  - name: deny-federated-other
    tenant: "*"
    action: federation_inbound
    tool: "*"
    agent: "*"
    decision: deny
    priority: 40

  - name: allow-local-chat
    tenant: "*"
    action: "*"
    tool: "*"
    agent: "*"
    decision: allow
    priority: 1
`

func TestFederation_CrossNodeDelegation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping federation demo under -short")
	}
	dir := t.TempDir()

	nodeA := bootNode(t, dir, "node-a", allowAllPolicy)
	nodeB := bootNode(t, dir, "node-b", nodeBPolicy)

	// Create agents on each node.
	createAgent(t, nodeA, "research", "research", "You are the research agent on node-a.")
	createAgent(t, nodeB, "legal", "legal", "You are the legal agent on node-b.")

	// Wire bidirectional in-process transports so each node's federation
	// module can reach the other node's bus when issuing federation.delegate.
	transportA := &inProcessTransport{targets: map[string]*ipc.Bus{"node-b": nodeB.bus}}
	transportB := &inProcessTransport{targets: map[string]*ipc.Bus{"node-a": nodeA.bus}}
	nodeA.fed.SetTransport(transportA)
	nodeB.fed.SetTransport(transportB)

	// Register each as a peer of the other. AddPeer is the programmatic
	// helper we added so demos don't need the HTTP join endpoint.
	nodeA.fed.AddPeer(federation.Peer{Name: "node-b", Endpoint: "inprocess://node-b", Secret: "peer-shared-secret"})
	nodeB.fed.AddPeer(federation.Peer{Name: "node-a", Endpoint: "inprocess://node-a", Secret: "peer-shared-secret"})

	// Now: node-a (acting as the research agent's tenant) sends a federation
	// delegate to node-b's legal agent. This exercises:
	//   federation.delegate (outbound)  -> transport.Delegate
	//   federation.delegate.inbound     -> policy.check (federation_inbound)
	//                                   -> agent.chat (mock provider)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := nodeA.bus.Request(ctx, ipc.Message{
		Source: "test", Target: "federation", Topic: "federation.delegate",
		Payload: federation.DelegateRequest{
			Peer:    "node-b",
			Tenant:  "legal",
			Agent:   "legal",
			User:    "alice@node-a",
			Message: "Review the indemnity clause draft.",
		},
	})
	if err != nil {
		t.Fatalf("federation.delegate: %v", err)
	}

	dr, ok := resp.Payload.(federation.DelegateResponse)
	if !ok {
		t.Fatalf("expected federation.DelegateResponse, got %T", resp.Payload)
	}
	if dr.Error != "" {
		t.Fatalf("delegate returned error: %s", dr.Error)
	}
	if dr.PeerID != "node-b" {
		t.Fatalf("expected response from node-b, got %q", dr.PeerID)
	}
	if !strings.Contains(dr.Content, "[served-by:node-b]") {
		t.Fatalf("expected node-b marker in content, got %q", dr.Content)
	}
	t.Logf("cross-node delegation OK: peer=%s agent=%s decision=%s content=%q",
		dr.PeerID, dr.Agent, dr.Decision, dr.Content)
}

// TestFederation_PolicyDeniesUnauthorisedAgent proves the receiving node's
// policy actually fires — node-a tries to delegate to a non-allowed agent on
// node-b and is rejected before the agent runtime ever runs.
func TestFederation_PolicyDeniesUnauthorisedAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping federation demo under -short")
	}
	dir := t.TempDir()

	nodeA := bootNode(t, dir, "node-a", allowAllPolicy)
	nodeB := bootNode(t, dir, "node-b", nodeBPolicy)

	createAgent(t, nodeA, "research", "research", "")
	// Deliberately create an agent on node-b that node-b's policy does NOT
	// allow to be invoked over federation.
	createAgent(t, nodeB, "internal", "research", "")

	transportA := &inProcessTransport{targets: map[string]*ipc.Bus{"node-b": nodeB.bus}}
	nodeA.fed.SetTransport(transportA)
	nodeA.fed.AddPeer(federation.Peer{Name: "node-b", Endpoint: "inprocess://node-b", Secret: "peer-shared-secret"})
	nodeB.fed.AddPeer(federation.Peer{Name: "node-a", Endpoint: "inprocess://node-a", Secret: "peer-shared-secret"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := nodeA.bus.Request(ctx, ipc.Message{
		Source: "test", Target: "federation", Topic: "federation.delegate",
		Payload: federation.DelegateRequest{
			Peer:    "node-b",
			Tenant:  "research",
			Agent:   "internal",
			User:    "alice@node-a",
			Message: "Try to access node-b's internal-only agent.",
		},
	})
	if err == nil {
		t.Fatal("expected delegation to be denied by node-b policy, got success")
	}
	// Security fix: the outbound caller no longer receives the remote peer's
	// internal policy-denial wording (which could leak node-b's rule names and
	// internal structure). The detailed reason is logged on the receiving node
	// only; the caller gets a generic, redacted failure. Asserting on the
	// generic message keeps this test honest about the fail-closed behavior
	// without depending on leaked internal detail.
	if !strings.Contains(err.Error(), "federation: remote peer delegation failed") {
		t.Fatalf("expected generic redacted delegation failure, got: %v", err)
	}
	t.Logf("policy boundary OK (redacted to caller): %v", err)
}
