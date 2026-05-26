package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/curator"
)

// curatorStubProvider is a tiny agent.ModelProvider for the judge
// HTTP test. Defined here to keep the curator → api test boundary
// clean without re-exporting the stub from the curator package.
type curatorStubProvider struct {
	reply string
}

func (p *curatorStubProvider) Name() string { return "stub" }
func (p *curatorStubProvider) Chat(ctx context.Context, msgs []agent.Message, tools []agent.ToolDef) (agent.Message, error) {
	return agent.Message{Role: agent.RoleAssistant, Content: p.reply}, nil
}

// fakeDisablerAPI mimics the skill registry's Disable surface for
// HTTP-level prune tests.
type fakeDisablerAPI struct {
	disabled map[string]string
}

func (f *fakeDisablerAPI) Disable(name, reason string) error {
	if f.disabled == nil {
		f.disabled = make(map[string]string)
	}
	if _, ok := f.disabled[name]; ok {
		return fmt.Errorf("skill %q already disabled", name)
	}
	f.disabled[name] = reason
	return nil
}

// fakeSnapshotAPI provides canned skills for the consolidate HTTP test.
type fakeSnapshotAPI struct {
	skills []curator.ConsolidationSkillSnapshot
}

func (f *fakeSnapshotAPI) SkillsForConsolidation() []curator.ConsolidationSkillSnapshot {
	return f.skills
}

// TestCuratorScoresHTTP exercises the GET /api/v1/curator/scores
// route end-to-end: HTTP handler → IPC → curator module → store.
func TestCuratorScoresHTTP(t *testing.T) {
	dir := t.TempDir()
	bus := ipc.NewBus()
	defer bus.Close()

	mod := curator.New(filepath.Join(dir, "curator.db"))
	ctx := context.Background()
	if err := mod.Init(ctx, &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := mod.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer mod.Stop(ctx)

	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		mod.Store().Record(curator.Invocation{
			SkillName: "skill-A", Tenant: "t", Agent: "g",
			Success: true, DurationMs: 10, Timestamp: now,
		})
	}

	srv := &Server{mux: http.NewServeMux(), bus: bus}
	srv.registerRoutes()

	// Unfiltered.
	req := httptest.NewRequest("GET", "/api/v1/curator/scores", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data []curator.SkillScore `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, rec.Body.String())
	}
	if len(env.Data) != 1 || env.Data[0].SkillName != "skill-A" {
		t.Fatalf("expected skill-A row, got %+v", env.Data)
	}
	if env.Data[0].SuccessRate != 100 {
		t.Fatalf("expected 100%% success, got %f", env.Data[0].SuccessRate)
	}

	// Filtered.
	req = httptest.NewRequest("GET", "/api/v1/curator/scores?skill_name=skill-A", nil)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("filtered: expected 200, got %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal filtered: %v", err)
	}
	if len(env.Data) != 1 || env.Data[0].SkillName != "skill-A" {
		t.Fatalf("filtered: expected skill-A, got %+v", env.Data)
	}
}

// startCuratorForAPITest spins up a curator module + bus + server
// scaffold used by the v1 HTTP-handler tests.
func startCuratorForAPITest(t *testing.T) (*curator.Module, *Server) {
	t.Helper()
	dir := t.TempDir()
	bus := ipc.NewBus()
	mod := curator.New(filepath.Join(dir, "curator.db"))
	mod.SetNow(func() time.Time { return time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC) })
	ctx := context.Background()
	if err := mod.Init(ctx, &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := mod.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		mod.Stop(ctx)
		bus.Close()
	})
	srv := &Server{mux: http.NewServeMux(), bus: bus}
	srv.registerRoutes()
	return mod, srv
}

func TestCuratorJudgeHTTP(t *testing.T) {
	mod, srv := startCuratorForAPITest(t)

	// Seed an invocation so the judge has a row to update.
	id, err := mod.Store().RecordID(curator.Invocation{
		SkillName: "judged", Success: true, DurationMs: 5,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	mod.SetJudge(curator.NewJudge(&curatorStubProvider{
		reply: `{"score": 0.42, "reason": "meh", "verdict": "acceptable"}`,
	}, "test"))

	body, _ := json.Marshal(curator.InvocationContext{
		SkillName:    "judged",
		InvocationID: id,
		UserMessage:  "hi",
		Success:      true,
	})
	req := httptest.NewRequest("POST", "/api/v1/curator/judge", bytes.NewReader(body))
	req.Header.Set("X-Cyntr-Admin", "1")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data curator.JudgeResult `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, rec.Body.String())
	}
	if env.Data.Score != 0.42 || env.Data.Verdict != curator.VerdictAcceptable {
		t.Fatalf("unexpected judge result %+v", env.Data)
	}
}

func TestCuratorJudgeHTTPRejectsNonAdmin(t *testing.T) {
	_, srv := startCuratorForAPITest(t)
	// Simulate a request that the auth middleware would have stamped
	// with a non-admin identity. We hand-build the context value so
	// the handler's admin gate fires.
	body, _ := json.Marshal(curator.InvocationContext{SkillName: "x"})
	req := httptest.NewRequest("POST", "/api/v1/curator/judge", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), contextKeyAuth, "apikey")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCuratorPruneHTTP(t *testing.T) {
	mod, srv := startCuratorForAPITest(t)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	// Seed a long-failing skill.
	for i := 0; i < 20; i++ {
		mod.Store().Record(curator.Invocation{
			SkillName:  "doomed",
			Success:    false,
			Error:      "boom",
			DurationMs: 10,
			Timestamp:  now.Add(-time.Duration(10*24-i*12) * time.Hour),
		})
	}
	mod.SetSkillDisabler(&fakeDisablerAPI{})

	req := httptest.NewRequest("POST", "/api/v1/curator/prune", nil)
	req.Header.Set("X-Cyntr-Admin", "1")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data curator.PruneReport `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(env.Data.Entries) != 1 || env.Data.Entries[0].Skill != "doomed" {
		t.Fatalf("expected doomed entry, got %+v", env.Data)
	}
	if !env.Data.Entries[0].Disabled {
		t.Fatalf("expected disabled=true, got %+v", env.Data.Entries[0])
	}
}

func TestCuratorPruneHTTPRejectsNonAdmin(t *testing.T) {
	_, srv := startCuratorForAPITest(t)
	req := httptest.NewRequest("POST", "/api/v1/curator/prune", nil)
	ctx := context.WithValue(req.Context(), contextKeyAuth, "apikey")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestCuratorConsolidateHTTP(t *testing.T) {
	mod, srv := startCuratorForAPITest(t)
	mod.SetSnapshotter(&fakeSnapshotAPI{
		skills: []curator.ConsolidationSkillSnapshot{
			{Name: "a", Tools: []string{"x", "y", "z"}, Invocations: 100},
			{Name: "b", Tools: []string{"x", "y", "z"}, Invocations: 100},
		},
	})

	req := httptest.NewRequest("GET", "/api/v1/curator/consolidate", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data curator.ConsolidationReport `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, rec.Body.String())
	}
	if len(env.Data.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %+v", env.Data.Suggestions)
	}
}
