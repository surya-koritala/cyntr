package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/curator"
)

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
