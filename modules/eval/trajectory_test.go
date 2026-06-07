package eval

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func newTestStore(t *testing.T) *TrajectoryStore {
	t.Helper()
	st, err := NewTrajectoryStore(filepath.Join(t.TempDir(), "traj.db"))
	if err != nil {
		t.Fatalf("NewTrajectoryStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func newTestModule(t *testing.T, opts ...TrajectoryOption) (*TrajectoryModule, *TrajectoryStore, *ipc.Bus) {
	t.Helper()
	st := newTestStore(t)
	bus := ipc.NewBus()
	t.Cleanup(bus.Close)
	m := NewTrajectoryModule(st, opts...)
	if err := m.Init(context.Background(), &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return m, st, bus
}

func sampleTurn() agent.TurnRecord {
	return agent.TurnRecord{
		Tenant:      "acme",
		User:        "alice",
		Session:     "sess_acme_assistant",
		Agent:       "assistant",
		Model:       "mock",
		UserMessage: "look up the weather",
		Response:    "It is sunny.",
		ToolsUsed:   []string{"http", "json_query"},
		ToolCalls:   2,
		Turns:       3,
		Outcome:     "ok",
	}
}

// waitFor polls cond until true or the deadline; the turn-event path is async.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func TestRecordingOffByDefault(t *testing.T) {
	_, st, bus := newTestModule(t)
	bus.Publish(ipc.Message{
		Source: "agent_runtime", Topic: agent.TopicTurnCompleted,
		Type: ipc.MessageTypeEvent, Payload: sampleTurn(),
	})
	// Give the async subscriber a chance to (not) write.
	time.Sleep(100 * time.Millisecond)
	n, err := st.Count("acme", "")
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Fatalf("recording off by default: expected 0 rows, got %d", n)
	}
}

func TestRecordedTurnYieldsCompleteTrajectory(t *testing.T) {
	m, st, bus := newTestModule(t)
	m.SetRecording("acme", "assistant", true)

	bus.Publish(ipc.Message{
		Source: "agent_runtime", Topic: agent.TopicTurnCompleted,
		Type: ipc.MessageTypeEvent, Payload: sampleTurn(),
	})

	if !waitFor(t, func() bool { n, _ := st.Count("acme", ""); return n == 1 }) {
		t.Fatal("expected one recorded trajectory")
	}
	trajs, err := st.List("acme", "", "", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	tr := trajs[0]
	if tr.Prompt != "look up the weather" || tr.Output != "It is sunny." {
		t.Fatalf("prompt/output not captured: %+v", tr)
	}
	if len(tr.Steps) != 2 || tr.Steps[0].Tool != "http" || tr.Steps[1].Tool != "json_query" {
		t.Fatalf("decision sequence not captured: %+v", tr.Steps)
	}
	if tr.ToolCalls != 2 || tr.Turns != 3 || tr.Outcome != "ok" {
		t.Fatalf("metadata not captured: %+v", tr)
	}
	if tr.Schema != TrajectorySchemaRaw {
		t.Fatalf("schema = %q", tr.Schema)
	}
}

func TestTenantIsolation(t *testing.T) {
	m, st, bus := newTestModule(t)
	m.SetRecording("acme", "assistant", true)
	m.SetRecording("globex", "assistant", true)

	acme := sampleTurn()
	globex := sampleTurn()
	globex.Tenant = "globex"
	globex.UserMessage = "globex secret prompt"

	for _, tr := range []agent.TurnRecord{acme, globex} {
		bus.Publish(ipc.Message{Source: "agent_runtime", Topic: agent.TopicTurnCompleted, Type: ipc.MessageTypeEvent, Payload: tr})
	}
	if !waitFor(t, func() bool { n, _ := st.Count("acme", ""); m2, _ := st.Count("globex", ""); return n == 1 && m2 == 1 }) {
		t.Fatal("expected one row per tenant")
	}

	// Listing acme must never surface globex's data.
	acmeRows, _ := st.List("acme", "", "", 10)
	for _, r := range acmeRows {
		if r.Tenant != "acme" {
			t.Fatalf("tenant leak: got %q in acme list", r.Tenant)
		}
		if strings.Contains(r.Prompt, "globex secret") {
			t.Fatal("globex prompt leaked into acme list")
		}
	}
	// A missing tenant is rejected, not treated as a global query.
	if _, err := st.List("", "", "", 10); err == nil {
		t.Fatal("List with empty tenant should error")
	}
}

func TestTenantWildcardRecording(t *testing.T) {
	m, st, bus := newTestModule(t)
	m.SetRecording("acme", "", true) // wildcard: every agent in acme

	tr := sampleTurn()
	tr.Agent = "other-agent"
	bus.Publish(ipc.Message{Source: "agent_runtime", Topic: agent.TopicTurnCompleted, Type: ipc.MessageTypeEvent, Payload: tr})

	if !waitFor(t, func() bool { n, _ := st.Count("acme", ""); return n == 1 }) {
		t.Fatal("wildcard recording should capture any agent in the tenant")
	}
}

func TestSecretsAndPIIStrippedOnInsert(t *testing.T) {
	st := newTestStore(t)
	err := st.Insert(Trajectory{
		Tenant: "acme",
		Agent:  "assistant",
		Prompt: "my key is AKIAIOSFODNN7EXAMPLE and email bob@example.com",
		Steps: []TrajectoryStep{
			{Index: 0, Tool: "http", Input: "token=ghp_abcdefghijklmnopqrstuvwxyz0123456789", Observation: "ssn 123-45-6789"},
		},
		Output: "done",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	rows, _ := st.List("acme", "", "", 1)
	tr := rows[0]
	if strings.Contains(tr.Prompt, "AKIA") || strings.Contains(tr.Prompt, "bob@example.com") {
		t.Fatalf("secret/PII not stripped from prompt: %q", tr.Prompt)
	}
	if strings.Contains(tr.Steps[0].Input, "ghp_") {
		t.Fatalf("secret not stripped from step input: %q", tr.Steps[0].Input)
	}
	if strings.Contains(tr.Steps[0].Observation, "123-45-6789") {
		t.Fatalf("PII not stripped from observation: %q", tr.Steps[0].Observation)
	}
}

func TestExportJSONL(t *testing.T) {
	m, _, bus := newTestModule(t)
	m.SetRecording("acme", "assistant", true)
	for i := 0; i < 3; i++ {
		bus.Publish(ipc.Message{Source: "agent_runtime", Topic: agent.TopicTurnCompleted, Type: ipc.MessageTypeEvent, Payload: sampleTurn()})
	}

	var resp ipc.Message
	if !waitFor(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		r, err := bus.Request(ctx, ipc.Message{
			Source: "test", Target: "trajectory", Topic: TopicTrajectoryExport,
			Payload: ExportRequest{Tenant: "acme"},
		})
		if err != nil {
			return false
		}
		resp = r
		jsonl, _ := r.Payload.(string)
		return strings.Count(strings.TrimSpace(jsonl), "\n") == 2 // 3 lines = 2 newlines
	}) {
		t.Fatalf("expected 3 JSONL records, got: %v", resp.Payload)
	}
}

func TestSetRecordingOverIPC(t *testing.T) {
	m, st, bus := newTestModule(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := bus.Request(ctx, ipc.Message{
		Source: "test", Target: "trajectory", Topic: TopicTrajectorySetRecording,
		Payload: SetRecordingRequest{Tenant: "acme", Agent: "assistant", On: true},
	}); err != nil {
		t.Fatalf("set_recording: %v", err)
	}
	if !m.isRecording("acme", "assistant") {
		t.Fatal("recording should be on after IPC toggle")
	}
	bus.Publish(ipc.Message{Source: "agent_runtime", Topic: agent.TopicTurnCompleted, Type: ipc.MessageTypeEvent, Payload: sampleTurn()})
	if !waitFor(t, func() bool { n, _ := st.Count("acme", ""); return n == 1 }) {
		t.Fatal("expected capture after IPC opt-in")
	}
}

func TestRecordRequestHonorsOptIn(t *testing.T) {
	m, st, _ := newTestModule(t)
	// Not opted in + not forced -> skipped.
	if _, err := m.handleRecord(ipc.Message{Payload: RecordRequest{Trajectory: Trajectory{Tenant: "acme", Agent: "a", Prompt: "x"}}}); err != nil {
		t.Fatalf("handleRecord: %v", err)
	}
	if n, _ := st.Count("acme", ""); n != 0 {
		t.Fatal("unforced record without opt-in should be skipped")
	}
	// Forced -> persisted even without opt-in (the run is the explicit opt-in).
	if _, err := m.handleRecord(ipc.Message{Payload: RecordRequest{Force: true, Trajectory: Trajectory{Tenant: "acme", Agent: "a", Prompt: "x"}}}); err != nil {
		t.Fatalf("handleRecord forced: %v", err)
	}
	if n, _ := st.Count("acme", ""); n != 1 {
		t.Fatal("forced record should persist")
	}
}
