package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/cyntr-dev/cyntr/auth"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent/tools"
)

// canvasTestServer mounts the CanvasWS handler on a mux with the {tid}/{sid}
// path params the handler reads.
func canvasTestServer(t *testing.T, ws *CanvasWS) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tenants/{tid}/canvas/{sid}/ws", ws.Handle)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func wsURL(base, tid, sid, key string) string {
	u := strings.Replace(base, "http://", "ws://", 1)
	u += "/api/v1/tenants/" + tid + "/canvas/" + sid + "/ws"
	if key != "" {
		u += "?key=" + key
	}
	return u
}

func mintToken(t *testing.T, sm *auth.SessionManager, tenant string) string {
	t.Helper()
	tok, err := sm.CreateToken(auth.Principal{ID: "u@x", Tenant: tenant, Roles: []string{auth.ScopeAgent}}, time.Hour)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return tok
}

func TestCanvasWS_RejectsUnauthenticated(t *testing.T) {
	sm := auth.NewSessionManager("secret")
	ws := NewCanvasWS(nil, tools.NewCanvasStore(), sm, true)
	srv := canvasTestServer(t, ws)

	_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "finance", "s1", ""), nil)
	if err == nil {
		t.Fatal("expected handshake failure for unauthenticated client")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %v", resp)
	}
}

func TestCanvasWS_RejectsCrossTenant(t *testing.T) {
	sm := auth.NewSessionManager("secret")
	ws := NewCanvasWS(nil, tools.NewCanvasStore(), sm, true)
	srv := canvasTestServer(t, ws)

	// Token is for tenant "finance" but client tries to open "marketing".
	tok := mintToken(t, sm, "finance")
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "marketing", "s1", tok), nil)
	if err == nil {
		t.Fatal("expected cross-tenant handshake failure")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %v", resp)
	}
}

func TestCanvasWS_RejectsBadToken(t *testing.T) {
	sm := auth.NewSessionManager("secret")
	ws := NewCanvasWS(nil, tools.NewCanvasStore(), sm, true)
	srv := canvasTestServer(t, ws)

	_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "finance", "s1", "garbage.token.here"), nil)
	if err == nil {
		t.Fatal("expected handshake failure for bad token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %v", resp)
	}
}

func TestCanvasWS_ReplayOnConnect(t *testing.T) {
	sm := auth.NewSessionManager("secret")
	store := tools.NewCanvasStore()
	// Persist a doc before any client connects.
	store.Put(&tools.CanvasDoc{
		Tenant:  "finance",
		Session: "s1",
		Nodes:   []tools.CanvasNode{{Type: "text", Text: "restored"}},
	})
	ws := NewCanvasWS(nil, store, sm, true)
	srv := canvasTestServer(t, ws)

	tok := mintToken(t, sm, "finance")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "finance", "s1", tok), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var doc tools.CanvasDoc
	if err := conn.ReadJSON(&doc); err != nil {
		t.Fatalf("read replay: %v", err)
	}
	if len(doc.Nodes) != 1 || doc.Nodes[0].Text != "restored" {
		t.Fatalf("bad replayed doc: %+v", doc)
	}
}

func TestCanvasWS_LiveBroadcastScopedToTenantSession(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	sm := auth.NewSessionManager("secret")
	store := tools.NewCanvasStore()
	ws := NewCanvasWS(bus, store, sm, true)
	srv := canvasTestServer(t, ws)

	// Two clients: same tenant, different sessions.
	tok := mintToken(t, sm, "finance")
	c1, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "finance", "s1", tok), nil)
	if err != nil {
		t.Fatalf("dial c1: %v", err)
	}
	defer c1.Close()
	c2, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "finance", "s2", tok), nil)
	if err != nil {
		t.Fatalf("dial c2: %v", err)
	}
	defer c2.Close()

	// Give subscriptions time to register.
	time.Sleep(50 * time.Millisecond)

	// Publish a doc for s1 only.
	doc := tools.CanvasDoc{Tenant: "finance", Session: "s1", Nodes: []tools.CanvasNode{{Type: "text", Text: "live"}}}
	store.Put(&doc)
	bus.Publish(ipc.Message{Topic: tools.CanvasTopic, Type: ipc.MessageTypeEvent, Payload: doc})

	// c1 (s1) should receive it.
	c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	var got tools.CanvasDoc
	if err := c1.ReadJSON(&got); err != nil {
		t.Fatalf("c1 read: %v", err)
	}
	if got.Nodes[0].Text != "live" {
		t.Fatalf("c1 bad doc: %+v", got)
	}

	// c2 (s2) should NOT receive it.
	c2.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if err := c2.ReadJSON(&got); err == nil {
		t.Fatal("c2 received a doc for a different session — scoping broken")
	}
}

func TestCanvasWS_APIKeyAuth(t *testing.T) {
	store := tools.NewCanvasStore()
	store.Put(&tools.CanvasDoc{Tenant: "finance", Session: "s1", Nodes: []tools.CanvasNode{{Type: "text", Text: "k"}}})
	ws := NewCanvasWS(nil, store, nil, true)
	ws.SetAPIKeys(map[string]auth.Principal{
		"cyntr_key": {Tenant: "finance", Scopes: []string{auth.ScopeAgent}},
	})
	srv := canvasTestServer(t, ws)

	// Wrong tenant for this key -> 403.
	_, resp, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "marketing", "s1", "cyntr_key"), nil)
	if err == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 cross-tenant for api key, got %v err=%v", resp, err)
	}

	// Correct tenant -> connects and replays.
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, "finance", "s1", "cyntr_key"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var doc tools.CanvasDoc
	if err := conn.ReadJSON(&doc); err != nil {
		t.Fatalf("read: %v", err)
	}
	if doc.Nodes[0].Text != "k" {
		t.Fatalf("bad doc: %+v", doc)
	}
}
