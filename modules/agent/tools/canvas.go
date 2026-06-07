package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

// CanvasTopic is the IPC bus topic on which canvas document updates are
// published. The web canvas WebSocket endpoint subscribes to this topic to
// live-broadcast updates to connected dashboard clients (ticket B9).
const CanvasTopic = "canvas.update"

// Supported A2UI node types. Start minimal; reject anything else so a
// compromised or buggy agent cannot inject arbitrary node types into a
// dashboard renderer.
var canvasNodeTypes = map[string]bool{
	"text":     true,
	"markdown": true,
	"table":    true,
	"image":    true,
	"button":   true,
}

// CanvasNode is one declarative A2UI element. Only the fields relevant to the
// node's Type are populated; the renderer ignores the rest.
type CanvasNode struct {
	Type string `json:"type"`
	// text / markdown
	Text string `json:"text,omitempty"`
	// table
	Columns []string   `json:"columns,omitempty"`
	Rows    [][]string `json:"rows,omitempty"`
	// image
	URL string `json:"url,omitempty"`
	Alt string `json:"alt,omitempty"`
	// button
	Label  string `json:"label,omitempty"`
	Action string `json:"action,omitempty"`
}

// CanvasDoc is a declarative A2UI document scoped to a (tenant, session).
type CanvasDoc struct {
	Tenant    string       `json:"tenant"`
	Session   string       `json:"session"`
	Title     string       `json:"title,omitempty"`
	Nodes     []CanvasNode `json:"nodes"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// validateDoc enforces the A2UI schema: a non-empty node list where every node
// is a known type with the fields that type requires. Unknown node types are
// rejected (fail closed).
func validateDoc(d *CanvasDoc) error {
	if d.Session == "" {
		return fmt.Errorf("canvas: session is required")
	}
	if len(d.Nodes) == 0 {
		return fmt.Errorf("canvas: document must contain at least one node")
	}
	for i, n := range d.Nodes {
		if !canvasNodeTypes[n.Type] {
			return fmt.Errorf("canvas: node %d has unknown type %q", i, n.Type)
		}
		switch n.Type {
		case "text", "markdown":
			if n.Text == "" {
				return fmt.Errorf("canvas: node %d (%s) requires non-empty text", i, n.Type)
			}
		case "table":
			if len(n.Columns) == 0 {
				return fmt.Errorf("canvas: node %d (table) requires columns", i)
			}
			for r, row := range n.Rows {
				if len(row) != len(n.Columns) {
					return fmt.Errorf("canvas: node %d (table) row %d has %d cells, want %d", i, r, len(row), len(n.Columns))
				}
			}
		case "image":
			if n.URL == "" {
				return fmt.Errorf("canvas: node %d (image) requires url", i)
			}
		case "button":
			if n.Label == "" {
				return fmt.Errorf("canvas: node %d (button) requires label", i)
			}
		}
	}
	return nil
}

// CanvasStore persists the current canvas document per (tenant, session). It is
// the source of truth replayed to a dashboard client on (re)connect so that the
// rendered state is restored after a reload or network blip.
//
// The store is deliberately tenant-scoped at the key level: a caller can only
// read or write a document for an explicit (tenant, session) pair, and the WS
// endpoint derives the tenant from the authenticated principal — never from
// client input — so one tenant can never read another tenant's canvas.
type CanvasStore struct {
	mu   sync.RWMutex
	docs map[string]*CanvasDoc // key: tenant + "\x00" + session
}

// NewCanvasStore creates an empty in-memory canvas store.
func NewCanvasStore() *CanvasStore {
	return &CanvasStore{docs: make(map[string]*CanvasDoc)}
}

func canvasKey(tenant, session string) string { return tenant + "\x00" + session }

// Put stores (replaces) the document for its (tenant, session).
func (s *CanvasStore) Put(d *CanvasDoc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *d
	s.docs[canvasKey(d.Tenant, d.Session)] = &cp
}

// Get returns the current document for (tenant, session), or ok=false if none
// has been rendered yet. The returned doc is a copy.
func (s *CanvasStore) Get(tenant, session string) (CanvasDoc, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.docs[canvasKey(tenant, session)]
	if !ok {
		return CanvasDoc{}, false
	}
	return *d, true
}

// CanvasRenderTool is an agent.Tool that accepts a small declarative A2UI JSON
// document, validates it, persists it per (tenant, session), and broadcasts it
// on the IPC bus for live dashboard rendering (ticket B9).
//
// Tenant is taken from the authenticated tool-execution context
// (agent.ToolCaller), never from tool input, so an agent cannot render into
// another tenant's canvas.
type CanvasRenderTool struct {
	bus   *ipc.Bus
	store *CanvasStore
}

// NewCanvasRenderTool wires the tool to the IPC bus and a canvas store. The same
// store instance should be handed to the web canvas WebSocket endpoint so that
// reconnecting clients replay the persisted document.
func NewCanvasRenderTool(bus *ipc.Bus, store *CanvasStore) *CanvasRenderTool {
	if store == nil {
		store = NewCanvasStore()
	}
	return &CanvasRenderTool{bus: bus, store: store}
}

// Store returns the underlying canvas store (so callers can share it with the
// WebSocket endpoint).
func (t *CanvasRenderTool) Store() *CanvasStore { return t.store }

func (t *CanvasRenderTool) Name() string { return "canvas_render" }

func (t *CanvasRenderTool) Description() string {
	return "Render a live A2UI canvas in the user's dashboard. Pass a JSON document with a 'nodes' array. " +
		"Supported node types: text {text}, markdown {text}, table {columns,rows}, image {url,alt}, button {label,action}. " +
		"The document replaces the previous canvas for this session and is broadcast to connected dashboards in real time."
}

func (t *CanvasRenderTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"session":  {Type: "string", Description: "Session/conversation ID this canvas belongs to", Required: true},
		"document": {Type: "string", Description: "A2UI JSON document: {\"title\":\"...\",\"nodes\":[{\"type\":\"text\",\"text\":\"hi\"}]}", Required: true},
	}
}

func (t *CanvasRenderTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	// Tenant is derived from the authenticated execution context — never from
	// tool input — to guarantee tenant isolation.
	tenant, _, _ := agent.ToolCaller(ctx)
	if tenant == "" {
		return "", fmt.Errorf("canvas: no tenant in execution context (fail closed)")
	}

	session := input["session"]
	if session == "" {
		return "", fmt.Errorf("canvas: session is required")
	}
	raw := input["document"]
	if raw == "" {
		return "", fmt.Errorf("canvas: document is required")
	}

	var doc CanvasDoc
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return "", fmt.Errorf("canvas: invalid document JSON: %w", err)
	}

	// Authoritative scoping: ignore any tenant/session embedded in the payload.
	doc.Tenant = tenant
	doc.Session = session
	doc.UpdatedAt = time.Now().UTC()

	if err := validateDoc(&doc); err != nil {
		return "", err
	}

	t.store.Put(&doc)

	if t.bus != nil {
		if err := t.bus.Publish(ipc.Message{
			Source:  "tool_canvas_render",
			Target:  "canvas",
			Type:    ipc.MessageTypeEvent,
			Topic:   CanvasTopic,
			Payload: doc,
		}); err != nil {
			return "", fmt.Errorf("canvas: broadcast: %w", err)
		}
	}

	return fmt.Sprintf("Canvas rendered for session %s (%d node(s))", session, len(doc.Nodes)), nil
}
