package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/node"
)

// TestNodePairIssueHTTP exercises POST /api/v1/node/pair end-to-end:
// HTTP handler → IPC node.pair.issue → node module → pairing store.
func TestNodePairIssueHTTP(t *testing.T) {
	dir := t.TempDir()
	bus := ipc.NewBus()
	defer bus.Close()

	mod := node.NewModule(filepath.Join(dir, "node_pairings.db"))
	ctx := context.Background()
	if err := mod.Init(ctx, &kernel.Services{Bus: bus}); err != nil {
		t.Fatalf("init: %v", err)
	}
	if err := mod.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer mod.Stop(ctx)

	srv := &Server{mux: http.NewServeMux(), bus: bus}
	srv.registerRoutes()

	// Happy path: a code is issued for the node.
	body, _ := json.Marshal(map[string]any{"node": "laptop", "capabilities": []string{"voice", "canvas"}})
	req := httptest.NewRequest("POST", "/api/v1/node/pair", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env struct {
		Data struct {
			Code   string `json:"code"`
			Node   string `json:"node"`
			Tenant string `json:"tenant"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, rec.Body.String())
	}
	if env.Data.Code == "" {
		t.Fatalf("expected a non-empty pairing code, got %+v", env.Data)
	}
	if env.Data.Node != "laptop" || env.Data.Tenant != "default" {
		t.Fatalf("unexpected node/tenant: %+v", env.Data)
	}

	// Missing node → 400.
	req = httptest.NewRequest("POST", "/api/v1/node/pair", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("expected 400 for missing node, got %d: %s", rec.Code, rec.Body.String())
	}
}
