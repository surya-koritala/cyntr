package skill

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

func startSkillRuntime(t *testing.T, autoActivateSafe bool) (*Runtime, *ipc.Bus) {
	t.Helper()
	bus := ipc.NewBus()
	t.Cleanup(bus.Close)
	cs, err := NewCandidateStore(filepath.Join(t.TempDir(), "cand.db"))
	if err != nil {
		t.Fatalf("NewCandidateStore: %v", err)
	}
	t.Cleanup(func() { cs.Close() })

	r := NewRuntime()
	r.SetCandidateStore(cs)
	r.SetAutoActivateSafe(autoActivateSafe)
	if err := r.Init(context.Background(), &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return r, bus
}

func propose(t *testing.T, bus *ipc.Bus, req ProposeRequest) ProposeResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := bus.Request(ctx, ipc.Message{Source: "t", Target: "skill_runtime", Topic: TopicPropose, Payload: req})
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	return resp.Payload.(ProposeResult)
}

func request(t *testing.T, bus *ipc.Bus, topic string, payload any) (ipc.Message, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return bus.Request(ctx, ipc.Message{Source: "t", Target: "skill_runtime", Topic: topic, Payload: payload})
}

func TestProposeSafeAutoActivates(t *testing.T) {
	r, bus := startSkillRuntime(t, true) // policy allows safe auto-activation
	res := propose(t, bus, ProposeRequest{
		Tenant: "acme", Name: "weather-helper", Instructions: "# Weather\nDo X.",
		Capabilities: Capabilities{Tools: []string{"http_request"}}, SourceAgent: "a1",
	})
	if !res.Activated || res.Status != CandidateApproved {
		t.Fatalf("safe candidate should auto-activate, got %+v", res)
	}
	if _, ok := r.registry.Get("weather-helper"); !ok {
		t.Fatal("activated skill should be in the registry")
	}
	// Loadable by skill_router (appears in List, instructions resolvable).
	if instr := r.registry.GetInstructions([]string{"weather-helper"}); instr["weather-helper"] == "" {
		t.Fatal("activated skill instructions not loadable by skill_router")
	}
}

func TestProposeShellNeverAutoActivates(t *testing.T) {
	r, bus := startSkillRuntime(t, true) // even with policy opt-in...
	res := propose(t, bus, ProposeRequest{
		Tenant: "acme", Name: "dangerous", Instructions: "# Danger",
		Capabilities: Capabilities{Shell: true}, SourceAgent: "a1",
	})
	if res.Activated || res.Status != CandidatePending {
		t.Fatalf("shell candidate must NOT auto-activate, got %+v", res)
	}
	if _, ok := r.registry.Get("dangerous"); ok {
		t.Fatal("shell candidate must not be installed without approval")
	}

	// Same for network capability.
	res = propose(t, bus, ProposeRequest{
		Tenant: "acme", Name: "net", Instructions: "# Net",
		Capabilities: Capabilities{Network: []string{"https://*"}}, SourceAgent: "a1",
	})
	if res.Activated {
		t.Fatal("network candidate must NOT auto-activate")
	}
}

func TestProposeStaysPendingWithoutPolicy(t *testing.T) {
	r, bus := startSkillRuntime(t, false) // policy off
	res := propose(t, bus, ProposeRequest{
		Tenant: "acme", Name: "safe-but-manual", Instructions: "# X",
		Capabilities: Capabilities{}, SourceAgent: "a1",
	})
	if res.Activated || res.Status != CandidatePending {
		t.Fatalf("without policy opt-in even safe candidates stay pending, got %+v", res)
	}
	if _, ok := r.registry.Get("safe-but-manual"); ok {
		t.Fatal("nothing should be installed when policy is off")
	}
}

func TestApproveInstallsEvenShellCandidate(t *testing.T) {
	r, bus := startSkillRuntime(t, false)
	res := propose(t, bus, ProposeRequest{
		Tenant: "acme", Name: "ops-runbook", Instructions: "# Ops",
		Capabilities: Capabilities{Shell: true}, SourceAgent: "a1",
	})
	if res.Status != CandidatePending {
		t.Fatalf("expected pending, got %+v", res)
	}
	// Operator approval is the override — it installs regardless of caps.
	if _, err := request(t, bus, TopicCandidateApprove, res.ID); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, ok := r.registry.Get("ops-runbook"); !ok {
		t.Fatal("approved candidate should be installed")
	}
	cand, _ := r.candidates.Get(res.ID)
	if cand.Status != CandidateApproved {
		t.Fatalf("candidate status = %q, want approved", cand.Status)
	}
	// Approving again must fail (no longer pending).
	if _, err := request(t, bus, TopicCandidateApprove, res.ID); err == nil {
		t.Fatal("re-approving a non-pending candidate should error")
	}
}

func TestRejectDoesNotInstall(t *testing.T) {
	r, bus := startSkillRuntime(t, false)
	res := propose(t, bus, ProposeRequest{
		Tenant: "acme", Name: "nope", Instructions: "# X", SourceAgent: "a1",
	})
	if _, err := request(t, bus, TopicCandidateReject, RejectRequest{ID: res.ID, Reason: "low quality"}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if _, ok := r.registry.Get("nope"); ok {
		t.Fatal("rejected candidate must not be installed")
	}
	cand, _ := r.candidates.Get(res.ID)
	if cand.Status != CandidateRejected || cand.Reason != "low quality" {
		t.Fatalf("reject not recorded: %+v", cand)
	}
}

func TestProposeValidation(t *testing.T) {
	_, bus := startSkillRuntime(t, false)
	if _, err := request(t, bus, TopicPropose, ProposeRequest{Name: "", Instructions: "x"}); err == nil {
		t.Fatal("empty name should be rejected (malformed manifest)")
	}
	if _, err := request(t, bus, TopicPropose, ProposeRequest{Name: "x", Instructions: ""}); err == nil {
		t.Fatal("empty instructions should be rejected")
	}
}

func TestProposeWithoutStoreErrors(t *testing.T) {
	bus := ipc.NewBus()
	t.Cleanup(bus.Close)
	r := NewRuntime() // no candidate store wired
	r.Init(context.Background(), &kernel.Services{Bus: bus})
	r.Start(context.Background())
	if _, err := request(t, bus, TopicPropose, ProposeRequest{Name: "x", Instructions: "y"}); err == nil {
		t.Fatal("propose without a candidate store should error")
	}
}

func TestCandidateStoreRoundTrip(t *testing.T) {
	cs, err := NewCandidateStore(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatalf("NewCandidateStore: %v", err)
	}
	defer cs.Close()

	id, err := cs.Propose(Candidate{
		Tenant: "acme", Name: "s1", Instructions: "# x",
		Capabilities: Capabilities{Network: []string{"https://api"}}, SourceAgent: "a1",
	})
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	got, err := cs.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Tenant != "acme" || got.Name != "s1" || !got.Capabilities.HasNetwork() {
		t.Fatalf("round-trip lost data: %+v", got)
	}
	pending, _ := cs.List(CandidatePending)
	if len(pending) != 1 {
		t.Fatalf("want 1 pending, got %d", len(pending))
	}
	if err := cs.SetStatus(id, CandidateApproved, ""); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if pending, _ := cs.List(CandidatePending); len(pending) != 0 {
		t.Fatalf("approved candidate should no longer be pending, got %d", len(pending))
	}
}
