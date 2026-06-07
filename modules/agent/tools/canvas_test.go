package tools

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func okDoc() string {
	return `{"title":"Hi","nodes":[{"type":"text","text":"hello"},{"type":"markdown","text":"# h"},{"type":"table","columns":["a","b"],"rows":[["1","2"]]},{"type":"image","url":"http://x/y.png"},{"type":"button","label":"Go","action":"go"}]}`
}

func TestCanvasRender_PersistsAndBroadcasts(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	got := make(chan CanvasDoc, 1)
	bus.Subscribe("test", CanvasTopic, func(m ipc.Message) (ipc.Message, error) {
		if d, ok := m.Payload.(CanvasDoc); ok {
			got <- d
		}
		return ipc.Message{}, nil
	})

	store := NewCanvasStore()
	tool := NewCanvasRenderTool(bus, store)

	ctx := agent.WithToolCaller(context.Background(), "finance", "a1", "u1")
	out, err := tool.Execute(ctx, map[string]string{"session": "s1", "document": okDoc()})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "5 node") {
		t.Fatalf("unexpected output: %q", out)
	}

	// Persisted under the tenant from context, not from payload.
	doc, ok := store.Get("finance", "s1")
	if !ok {
		t.Fatal("doc not persisted")
	}
	if doc.Tenant != "finance" || doc.Session != "s1" || len(doc.Nodes) != 5 {
		t.Fatalf("bad persisted doc: %+v", doc)
	}

	// Broadcast on the bus (publish is async).
	select {
	case d := <-got:
		if d.Tenant != "finance" || d.Session != "s1" {
			t.Fatalf("bad broadcast doc: %+v", d)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no broadcast received")
	}
}

func TestCanvasRender_TenantFromContextNotPayload(t *testing.T) {
	store := NewCanvasStore()
	tool := NewCanvasRenderTool(nil, store)

	// Payload tries to claim tenant "evil"; context says "finance".
	payload := `{"tenant":"evil","session":"ZZZ","nodes":[{"type":"text","text":"x"}]}`
	ctx := agent.WithToolCaller(context.Background(), "finance", "a1", "u1")
	// DisallowUnknownFields => "tenant"/"session" in payload are rejected as
	// unknown? They are valid json tags on CanvasDoc, so they decode but are
	// overwritten. Either way the stored tenant must be from context.
	_, err := tool.Execute(ctx, map[string]string{"session": "s9", "document": payload})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if _, ok := store.Get("evil", "s9"); ok {
		t.Fatal("doc stored under payload-claimed tenant — isolation broken")
	}
	if _, ok := store.Get("finance", "s9"); !ok {
		t.Fatal("doc not stored under context tenant")
	}
}

func TestCanvasRender_RejectsNoTenant(t *testing.T) {
	tool := NewCanvasRenderTool(nil, NewCanvasStore())
	_, err := tool.Execute(context.Background(), map[string]string{"session": "s1", "document": okDoc()})
	if err == nil {
		t.Fatal("expected error with no tenant in context")
	}
}

func TestCanvasRender_Validation(t *testing.T) {
	tool := NewCanvasRenderTool(nil, NewCanvasStore())
	ctx := agent.WithToolCaller(context.Background(), "finance", "a1", "u1")

	cases := []struct {
		name string
		doc  string
		ok   bool
	}{
		{"valid", okDoc(), true},
		{"unknown node type", `{"nodes":[{"type":"video","url":"x"}]}`, false},
		{"empty nodes", `{"nodes":[]}`, false},
		{"text missing text", `{"nodes":[{"type":"text"}]}`, false},
		{"table no columns", `{"nodes":[{"type":"table","rows":[["1"]]}]}`, false},
		{"table ragged row", `{"nodes":[{"type":"table","columns":["a","b"],"rows":[["1"]]}]}`, false},
		{"image no url", `{"nodes":[{"type":"image"}]}`, false},
		{"button no label", `{"nodes":[{"type":"button","action":"x"}]}`, false},
		{"malformed json", `{"nodes": [`, false},
		{"unknown field", `{"nodes":[{"type":"text","text":"x","bogus":1}]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(ctx, map[string]string{"session": "s1", "document": tc.doc})
			if tc.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestCanvasRender_RequiresSession(t *testing.T) {
	tool := NewCanvasRenderTool(nil, NewCanvasStore())
	ctx := agent.WithToolCaller(context.Background(), "finance", "a1", "u1")
	if _, err := tool.Execute(ctx, map[string]string{"document": okDoc()}); err == nil {
		t.Fatal("expected error when session missing")
	}
}

func TestCanvasStore_Isolation(t *testing.T) {
	s := NewCanvasStore()
	s.Put(&CanvasDoc{Tenant: "t1", Session: "s", Nodes: []CanvasNode{{Type: "text", Text: "a"}}})
	s.Put(&CanvasDoc{Tenant: "t2", Session: "s", Nodes: []CanvasNode{{Type: "text", Text: "b"}}})

	d1, _ := s.Get("t1", "s")
	d2, _ := s.Get("t2", "s")
	if d1.Nodes[0].Text != "a" || d2.Nodes[0].Text != "b" {
		t.Fatal("cross-tenant store bleed")
	}
	if _, ok := s.Get("t3", "s"); ok {
		t.Fatal("unexpected doc for unknown tenant")
	}
}

func TestCanvasStore_ConcurrentSafe(t *testing.T) {
	s := NewCanvasStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Put(&CanvasDoc{Tenant: "t", Session: "s", Nodes: []CanvasNode{{Type: "text", Text: "x"}}})
			s.Get("t", "s")
		}()
	}
	wg.Wait()
}
