package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/usermodel"
)

// fixedProvider mirrors the usermodel test fake but reduced to what the API
// test needs — a deterministic distill response. Kept here rather than
// imported to avoid the api package taking a hard dependency on usermodel
// internals.
type fixedProvider struct{ body string }

func (f fixedProvider) Name() string { return "fixed" }
func (f fixedProvider) DistillChat(ctx context.Context, _ []usermodel.DistillMessage) (string, int, int, error) {
	return f.body, 10, 5, nil
}

func TestProfileDistillEndpointReturnsResult(t *testing.T) {
	dir := t.TempDir()
	store, err := usermodel.NewStore(filepath.Join(dir, "usermodel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Seed: tenant opted-in + enough activity to trigger a real distill.
	if err := store.SetTenantDistillEnabled("acme", true); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		store.RecordActivity("acme", "alice", "user did something interesting")
	}

	bus := ipc.NewBus()
	defer bus.Close()

	d, err := usermodel.NewDistiller(usermodel.DistillerOptions{
		Store: store, Provider: fixedProvider{body: "## P\nfresh"}, Model: "fixed",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := usermodel.New(store)
	m.SetDistiller(d)
	if err := m.Init(context.Background(), &kernel.Services{Bus: bus}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer m.Stop(context.Background())

	srv := NewServer(bus, nil)
	req := httptest.NewRequest("POST", "/api/v1/tenants/acme/users/alice/profile/distill", http.NoBody)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var env Envelope
	if err := json.NewDecoder(w.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error != nil {
		t.Fatalf("unexpected error: %+v", env.Error)
	}
	// Data is a generic map after JSON round-trip.
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected object data, got %T", env.Data)
	}
	if data["tenant"] != "acme" || data["user"] != "alice" {
		t.Errorf("unexpected ids in result: %+v", data)
	}
	newSize, _ := data["new_size"].(float64)
	if newSize == 0 {
		t.Errorf("expected new_size > 0, got %v", data["new_size"])
	}
}

func TestProfileDistillEndpointReturns503WhenModuleAbsent(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	srv := NewServer(bus, nil)
	req := httptest.NewRequest("POST", "/api/v1/tenants/acme/users/alice/profile/distill", http.NoBody)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 503 {
		t.Fatalf("expected 503 when usermodel module not registered, got %d", w.Code)
	}
}
