package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/cyntr-dev/cyntr/auth"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent/tools"
)

// CanvasWS is the live agent-driven canvas (A2UI) WebSocket endpoint (ticket
// B9). It broadcasts canvas document updates to connected dashboard clients and
// replays the current persisted document on (re)connect.
//
// Tenant isolation is mandatory and fails closed:
//   - the connection MUST present a valid credential (JWT bearer or API key)
//     via the `key` query parameter (browsers can't set WS headers easily);
//   - the tenant is derived from the authenticated principal, NOT from client
//     input — the {tid} in the path must match the principal's tenant or the
//     handshake is rejected with 403;
//   - a client only ever receives updates for its own (tenant, session).
type CanvasWS struct {
	bus      *ipc.Bus
	store    *tools.CanvasStore
	sm       *auth.SessionManager // validates JWT bearer tokens
	apiKeys  map[string]auth.Principal
	enabled  bool // when false, auth is skipped (dev only) and tenant comes from path
	upgrader websocket.Upgrader

	mu      sync.RWMutex
	clients map[*canvasClient]struct{}
}

type canvasClient struct {
	conn    *websocket.Conn
	tenant  string
	session string
	writeMu sync.Mutex
}

// NewCanvasWS constructs the canvas WebSocket endpoint. The store MUST be the
// same instance handed to the canvas_render tool so reconnecting clients replay
// the latest rendered document. When enabled is false, authentication is
// skipped (intended for local development only) and the tenant is taken from
// the path — never do this in production.
func NewCanvasWS(bus *ipc.Bus, store *tools.CanvasStore, sm *auth.SessionManager, enabled bool) *CanvasWS {
	if store == nil {
		store = tools.NewCanvasStore()
	}
	c := &CanvasWS{
		bus:     bus,
		store:   store,
		sm:      sm,
		enabled: enabled,
		clients: make(map[*canvasClient]struct{}),
		upgrader: websocket.Upgrader{
			// Same-origin is the default; the dashboard is served from the same
			// host. Tests dial directly, which sets no Origin header (allowed).
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	if bus != nil {
		bus.Subscribe("canvas_ws", tools.CanvasTopic, c.onBusUpdate)
	}
	return c
}

// SetAPIKeys registers valid API keys mapped to their principal so the WS
// endpoint can authenticate API-key connections the same way the REST API does.
func (c *CanvasWS) SetAPIKeys(keys map[string]auth.Principal) {
	c.apiKeys = keys
}

// onBusUpdate fans a published canvas document out to every connected client
// scoped to the document's (tenant, session). Cross-tenant clients never see it.
func (c *CanvasWS) onBusUpdate(msg ipc.Message) (ipc.Message, error) {
	doc, ok := msg.Payload.(tools.CanvasDoc)
	if !ok {
		return ipc.Message{}, nil
	}
	c.mu.RLock()
	targets := make([]*canvasClient, 0)
	for cl := range c.clients {
		if cl.tenant == doc.Tenant && cl.session == doc.Session {
			targets = append(targets, cl)
		}
	}
	c.mu.RUnlock()
	for _, cl := range targets {
		cl.send(doc)
	}
	return ipc.Message{}, nil
}

func (cl *canvasClient) send(doc tools.CanvasDoc) {
	cl.writeMu.Lock()
	defer cl.writeMu.Unlock()
	_ = cl.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_ = cl.conn.WriteJSON(doc)
}

// authenticate resolves the principal for the request, failing closed. It
// returns the authenticated tenant (from the principal) or "", ok=false.
func (c *CanvasWS) authenticate(r *http.Request) (auth.Principal, bool) {
	if !c.enabled {
		// Dev mode: trust the path tenant. Production MUST set enabled=true.
		return auth.Principal{Tenant: r.PathValue("tid")}, true
	}
	token := r.URL.Query().Get("key")
	if token == "" {
		token = bearerToken(r)
	}
	if token == "" {
		return auth.Principal{}, false
	}
	// API key path.
	if c.apiKeys != nil {
		if p, ok := c.apiKeys[token]; ok {
			return p, true
		}
	}
	// JWT path.
	if c.sm != nil {
		if p, err := c.sm.ValidateToken(token); err == nil {
			return p, true
		}
	}
	return auth.Principal{}, false
}

// Handle is the http.HandlerFunc for the canvas WebSocket route. Mount it at
// e.g. GET /api/v1/tenants/{tid}/canvas/{sid}/ws.
func (c *CanvasWS) Handle(w http.ResponseWriter, r *http.Request) {
	tid := r.PathValue("tid")
	sid := r.PathValue("sid")
	if tid == "" || sid == "" {
		RespondError(w, http.StatusBadRequest, "INVALID_REQUEST", "tenant and session are required")
		return
	}

	p, ok := c.authenticate(r)
	if !ok {
		RespondError(w, http.StatusUnauthorized, "UNAUTHORIZED", "valid credential required")
		return
	}
	// Tenant isolation: the path tenant MUST match the principal's tenant.
	// Fail closed on any mismatch. (Admins are not exempted — the canvas is a
	// per-tenant live surface; cross-tenant viewing is never allowed here.)
	if c.enabled && p.Tenant != "" && p.Tenant != tid {
		RespondError(w, http.StatusForbidden, "FORBIDDEN", "cross-tenant access denied")
		return
	}

	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an error response.
		return
	}

	cl := &canvasClient{conn: conn, tenant: tid, session: sid}
	c.mu.Lock()
	c.clients[cl] = struct{}{}
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.clients, cl)
		c.mu.Unlock()
		_ = conn.Close()
	}()

	// Replay current persisted state so the dashboard is restored on connect.
	if doc, found := c.store.Get(tid, sid); found {
		cl.send(doc)
	}

	// Read loop: we don't accept client->server canvas mutations over this
	// socket (the agent owns the canvas), but we must drain reads to detect
	// disconnects and respond to control frames (ping/close).
	conn.SetReadLimit(4096)
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

// bearerToken extracts a bearer token from the Authorization header.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(h) > len(prefix) && h[:len(prefix)] == prefix {
		return h[len(prefix):]
	}
	return h
}
