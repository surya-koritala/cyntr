package node

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/audit"
)

// PairingStore persists tenant-scoped node pairings and pending pairing codes.
// It mirrors the semantics of modules/channel/pairing.go: an operator issues a
// short code for a (tenant, node), the node presents the code to pair, and the
// pairing is then a long-lived tenant-scoped grant. Codes are single-use — a
// successful pair consumes the pending code. Everything is keyed by tenant so a
// code issued for one tenant can never authenticate a node into another.
type PairingStore struct {
	mu sync.Mutex
	db *sql.DB
}

// NewPairingStore opens (or creates) the node pairing store at dbPath.
func NewPairingStore(dbPath string) (*PairingStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("node/pairing: open db: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS node_pairings (
			tenant TEXT NOT NULL, node TEXT NOT NULL,
			capabilities TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			PRIMARY KEY (tenant, node)
		)`,
		`CREATE TABLE IF NOT EXISTS node_pending (
			code TEXT PRIMARY KEY,
			tenant TEXT NOT NULL, node TEXT NOT NULL,
			capabilities TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			db.Close()
			return nil, fmt.Errorf("node/pairing: schema: %w", err)
		}
	}
	return &PairingStore{db: db}, nil
}

func (s *PairingStore) Close() error { return s.db.Close() }

// IssueCode creates a pending pairing code for (tenant, node) granting the
// given allowed capabilities once paired, and returns the code. Re-issuing for
// the same (tenant, node) leaves any prior pending code in place but adds a new
// one; codes are unique. An empty allowedCaps grants the full capability set.
func (s *PairingStore) IssueCode(tenant, node string, allowedCaps []string) (string, error) {
	if tenant == "" || node == "" {
		return "", fmt.Errorf("node/pairing: tenant and node are required")
	}
	if len(allowedCaps) == 0 {
		allowedCaps = allCapabilities
	}
	code := pairingCode()
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT INTO node_pending (code, tenant, node, capabilities, created_at) VALUES (?, ?, ?, ?, ?)`,
		code, tenant, node, strings.Join(allowedCaps, ","), time.Now().Unix())
	if err != nil {
		return "", fmt.Errorf("node/pairing: issue code: %w", err)
	}
	return code, nil
}

// Pairing is a resolved node pairing record.
type Pairing struct {
	Tenant string
	Node   string
	// AllowedCaps is the capability set this node may negotiate.
	AllowedCaps []string
}

// Redeem validates a pairing code and, on success, consumes the pending code
// and persists a long-lived pairing. It returns the bound Pairing. A missing,
// unknown, or whitespace-only code fails closed with an error and never returns
// a pairing.
func (s *PairingStore) Redeem(code string) (Pairing, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return Pairing{}, fmt.Errorf("node/pairing: empty code")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var p Pairing
	var caps string
	err := s.db.QueryRow(
		`SELECT tenant, node, capabilities FROM node_pending WHERE code=?`, code).
		Scan(&p.Tenant, &p.Node, &caps)
	if err == sql.ErrNoRows {
		return Pairing{}, fmt.Errorf("node/pairing: invalid code")
	}
	if err != nil {
		return Pairing{}, err
	}
	p.AllowedCaps = splitCaps(caps)
	if _, err := s.db.Exec(
		`INSERT INTO node_pairings (tenant, node, capabilities, created_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(tenant, node) DO UPDATE SET capabilities=excluded.capabilities, created_at=excluded.created_at`,
		p.Tenant, p.Node, caps, time.Now().Unix()); err != nil {
		return Pairing{}, fmt.Errorf("node/pairing: persist: %w", err)
	}
	s.db.Exec(`DELETE FROM node_pending WHERE code=?`, code)
	return p, nil
}

// AllowedCaps returns the capabilities granted to an already-paired node, and
// whether the node is paired at all. Used to re-authorize reconnecting nodes.
func (s *PairingStore) AllowedCaps(tenant, node string) ([]string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var caps string
	err := s.db.QueryRow(`SELECT capabilities FROM node_pairings WHERE tenant=? AND node=?`,
		tenant, node).Scan(&caps)
	if err != nil {
		return nil, false
	}
	return splitCaps(caps), true
}

func splitCaps(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pairingCode returns a short, unambiguous uppercase code (no I/O/0/1/L),
// matching the channel pairing alphabet.
func pairingCode() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 6)
	rand.Read(buf)
	var b strings.Builder
	for _, c := range buf {
		b.WriteByte(alphabet[int(c)%len(alphabet)])
	}
	return b.String()
}

// --- WebSocket server ---------------------------------------------------------

// Session is an authenticated, tenant-scoped node connection.
type Session struct {
	Tenant       string
	Node         string
	ID           string
	Capabilities []string // negotiated set
}

// hasCap reports whether the session negotiated capability c.
func (s *Session) hasCap(c string) bool {
	for _, k := range s.Capabilities {
		if k == c {
			return true
		}
	}
	return false
}

// Server upgrades WebSocket connections and drives the node protocol. It is
// tenant-agnostic at the transport layer: the tenant is established purely from
// the validated pairing code, so a connection that never presents a valid code
// is never bound to any tenant and is closed.
type Server struct {
	store    *PairingStore
	upgrader websocket.Upgrader
	audit    *audit.Emitter // optional; nil disables audit emission

	mu       sync.Mutex
	sessions map[string]*Session // session id -> session (for introspection/tests)
}

// NewServer builds a node WebSocket server backed by the given pairing store.
// emitter may be nil (audit disabled).
func NewServer(store *PairingStore, emitter *audit.Emitter) *Server {
	return &Server{
		store:    store,
		audit:    emitter,
		sessions: map[string]*Session{},
		upgrader: websocket.Upgrader{
			// Same-origin is enforced by the API auth middleware / reverse proxy;
			// the reference client is served from the same origin. Tighten in
			// deployment via CYNTR_NODE_ALLOWED_ORIGINS if cross-origin is needed.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// Sessions returns a snapshot of live sessions (test/introspection helper).
func (s *Server) Sessions() []Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		out = append(out, *sess)
	}
	return out
}

func (s *Server) addSession(sess *Session) {
	s.mu.Lock()
	s.sessions[sess.ID] = sess
	s.mu.Unlock()
}

func (s *Server) removeSession(id string) {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
}

// handshakeDeadline bounds how long a client has to complete pairing+hello.
const handshakeDeadline = 30 * time.Second

// ServeHTTP upgrades the request to a WebSocket and runs the protocol.
//
// GET /api/v1/node/ws
//
// The connection is unauthenticated at the transport layer; authentication is
// the pairing handshake itself. Fail-closed: any deviation (missing code, bad
// code, frame before pairing, ungranted capability) ends the connection.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade already wrote an error response.
	}
	s.handle(&wsConn{c: conn})
}

// nodeConn abstracts the websocket so tests can drive the protocol with an
// in-memory stub instead of a real network connection.
type nodeConn interface {
	ReadFrame() (Frame, error)
	WriteFrame(Frame) error
	SetReadDeadline(time.Time) error
	Close() error
}

// wsConn adapts a gorilla websocket to nodeConn.
type wsConn struct{ c *websocket.Conn }

func (w *wsConn) ReadFrame() (Frame, error) {
	var f Frame
	err := w.c.ReadJSON(&f)
	return f, err
}
func (w *wsConn) WriteFrame(f Frame) error          { return w.c.WriteJSON(f) }
func (w *wsConn) SetReadDeadline(t time.Time) error { return w.c.SetReadDeadline(t) }
func (w *wsConn) Close() error                      { return w.c.Close() }

// handle runs the full protocol over conn. It returns the established session
// (or nil) primarily so tests can assert on the negotiated result.
func (s *Server) handle(conn nodeConn) *Session {
	defer conn.Close()

	// Phase 1: pairing must be the first frame, within the deadline.
	conn.SetReadDeadline(time.Now().Add(handshakeDeadline))
	first, err := conn.ReadFrame()
	if err != nil {
		return nil
	}
	if err := validatePairFrame(first); err != nil {
		conn.WriteFrame(errFrame(ErrProtocol, err.Error()))
		return nil
	}
	pairing, err := s.store.Redeem(first.Code)
	if err != nil {
		// Fail closed: no tenant is ever bound for a bad code.
		conn.WriteFrame(errFrame(ErrBadPairing, "pairing failed"))
		s.emitAuth("", first.Node, "node_pair", "rejected")
		return nil
	}
	s.emitAuth(pairing.Tenant, pairing.Node, "node_pair", "success")

	// Phase 2: capability negotiation. The client may include capabilities on
	// the pair frame itself, or send a separate hello. Either way the offer is
	// intersected with what the pairing grants.
	offered := first.Capabilities
	if len(offered) == 0 {
		conn.SetReadDeadline(time.Now().Add(handshakeDeadline))
		hello, err := conn.ReadFrame()
		if err != nil {
			return nil
		}
		if hello.Type != MsgHello {
			conn.WriteFrame(errFrame(ErrProtocol, "expected hello frame after pairing"))
			return nil
		}
		offered = hello.Capabilities
	}

	negotiated := negotiateCapabilities(offered, pairing.AllowedCaps)
	sess := &Session{
		Tenant:       pairing.Tenant,
		Node:         pairing.Node,
		ID:           "ns_" + pairingCode(),
		Capabilities: negotiated,
	}
	s.addSession(sess)
	defer s.removeSession(sess.ID)

	if err := conn.WriteFrame(Frame{
		Type:         MsgWelcome,
		Tenant:       sess.Tenant,
		Session:      sess.ID,
		Capabilities: negotiated,
	}); err != nil {
		return sess
	}
	s.emitNegotiation(sess)

	// Phase 3: message loop. Only negotiated-capability frames are accepted;
	// tenant/session on inbound frames are ignored — the server uses the bound
	// session, so a client cannot reach another tenant.
	for {
		conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		f, err := conn.ReadFrame()
		if err != nil {
			return sess
		}
		if !s.dispatch(conn, sess, f) {
			return sess
		}
	}
}

// dispatch handles one post-handshake frame. It returns false to terminate the
// connection (used for fatal protocol violations).
func (s *Server) dispatch(conn nodeConn, sess *Session, f Frame) bool {
	switch f.Type {
	case MsgPing:
		conn.WriteFrame(Frame{Type: MsgPong})
	case MsgVoice:
		if !sess.hasCap(CapVoice) {
			conn.WriteFrame(errFrame(ErrNotNegotiated, "voice capability not negotiated"))
			return true
		}
		// Echo an ack carrying the bound tenant/session (server-authoritative).
		conn.WriteFrame(Frame{Type: MsgAck, Ref: MsgVoice, Tenant: sess.Tenant, Session: sess.ID})
	case MsgCanvas:
		if !sess.hasCap(CapCanvas) {
			conn.WriteFrame(errFrame(ErrNotNegotiated, "canvas capability not negotiated"))
			return true
		}
		conn.WriteFrame(Frame{Type: MsgAck, Ref: MsgCanvas, Tenant: sess.Tenant, Session: sess.ID})
	case MsgPair, MsgHello:
		// Re-pairing / re-hello mid-session is a protocol error: the tenant is
		// already bound and immutable for the life of the connection.
		conn.WriteFrame(errFrame(ErrProtocol, "already paired"))
	default:
		conn.WriteFrame(errFrame(ErrProtocol, "unknown frame type: "+f.Type))
	}
	return true
}

func (s *Server) emitAuth(tenant, node, eventType, result string) {
	if s.audit == nil {
		return
	}
	s.audit.Emit(audit.Entry{
		Tenant:    tenant,
		Principal: audit.Principal{User: node},
		Action:    audit.Action{Type: eventType, Module: "node", Detail: map[string]string{"node": node}},
		Result:    audit.Result{Status: result},
	})
}

func (s *Server) emitNegotiation(sess *Session) {
	if s.audit == nil {
		return
	}
	s.audit.Emit(audit.Entry{
		Tenant:    sess.Tenant,
		Principal: audit.Principal{User: sess.Node},
		Action: audit.Action{Type: "node_capabilities", Module: "node", Detail: map[string]string{
			"node":         sess.Node,
			"session":      sess.ID,
			"capabilities": strings.Join(sess.Capabilities, ","),
		}},
		Result: audit.Result{Status: "negotiated"},
	})
}

// --- kernel.Module ------------------------------------------------------------

// Module wires the node protocol into the kernel lifecycle. It owns the pairing
// store and a Server. The web layer obtains the Server via the kernel/IPC or by
// being handed the same instance at construction (see registration snippet).
type Module struct {
	dbPath string
	store  *PairingStore
	server *Server
	bus    *ipc.Bus
}

// NewModule builds the node module. dbPath is the SQLite path for pairings;
// empty defaults to "node_pairings.db".
func NewModule(dbPath string) *Module {
	if dbPath == "" {
		dbPath = "node_pairings.db"
	}
	return &Module{dbPath: dbPath}
}

func (m *Module) Name() string           { return "node" }
func (m *Module) Dependencies() []string { return nil }

func (m *Module) Init(ctx context.Context, services *kernel.Services) error {
	store, err := NewPairingStore(m.dbPath)
	if err != nil {
		return err
	}
	m.store = store
	m.bus = services.Bus
	m.server = NewServer(store, audit.NewEmitter(services.Bus, "node"))

	// Operators issue pairing codes over the bus, mirroring channel pairing.
	services.Bus.Handle("node", "node.pair.issue", func(msg ipc.Message) (ipc.Message, error) {
		req, _ := msg.Payload.(IssueRequest)
		code, err := m.store.IssueCode(req.Tenant, req.Node, req.Capabilities)
		if err != nil {
			return ipc.Message{Type: ipc.MessageTypeResponse, Payload: IssueResponse{Error: err.Error()}}, nil
		}
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: IssueResponse{Code: code}}, nil
	})
	return nil
}

func (m *Module) Start(ctx context.Context) error { return nil }

func (m *Module) Stop(ctx context.Context) error {
	if m.store != nil {
		return m.store.Close()
	}
	return nil
}

func (m *Module) Health(ctx context.Context) kernel.HealthStatus {
	if m.store == nil || m.server == nil {
		return kernel.HealthStatus{Healthy: false, Message: "node module not initialized"}
	}
	return kernel.HealthStatus{Healthy: true, Message: "ok"}
}

// Server exposes the WebSocket handler so the web layer can mount it.
func (m *Module) Server() *Server { return m.server }

// Store exposes the pairing store (e.g. for an operator REST endpoint).
func (m *Module) Store() *PairingStore { return m.store }

// IssueRequest / IssueResponse are the bus payloads for "node.pair.issue".
type IssueRequest struct {
	Tenant       string   `json:"tenant"`
	Node         string   `json:"node"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type IssueResponse struct {
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}
