package eval

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

// IPC topics owned by the trajectory subsystem. All payloads are tenant-scoped.
const (
	// TopicTrajectoryRecord lets the `trajectory run` fan-out (and any trusted
	// in-process producer) persist a fully-detailed trajectory directly,
	// bypassing the turn-event path so it can carry per-tool I/O.
	TopicTrajectoryRecord = "trajectory.record"
	// TopicTrajectoryExport returns matching trajectories as JSONL text.
	TopicTrajectoryExport = "trajectory.export"
	// TopicTrajectorySetRecording toggles opt-in recording for (tenant, agent).
	TopicTrajectorySetRecording = "trajectory.set_recording"
)

// RecordRequest carries a fully-detailed trajectory to persist. Used by the
// `trajectory run` fan-out path, which has the tool I/O the turn event lacks.
type RecordRequest struct {
	Trajectory Trajectory `json:"trajectory"`
	// Force records even when (tenant, agent) has not opted in. The fan-out
	// runner sets this because the run itself is the explicit opt-in; the live
	// turn-event subscriber never sets it (recording stays OFF by default).
	Force bool `json:"force"`
}

// SetRecordingRequest opts a (tenant, agent) in or out of live trajectory
// capture. Recording is OFF by default for every tenant/agent.
type SetRecordingRequest struct {
	Tenant string `json:"tenant"`
	Agent  string `json:"agent"` // "" means all agents in the tenant
	On     bool   `json:"on"`
}

// ExportRequest selects trajectories to export as JSONL. Tenant is mandatory.
type ExportRequest struct {
	Tenant string `json:"tenant"`
	Agent  string `json:"agent,omitempty"`
	RunID  string `json:"run_id,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// TrajectoryModule subscribes to agent.turn_completed and, for opted-in
// (tenant, agent) pairs, persists the full trajectory. Recording is opt-in and
// OFF by default — if nothing opts a tenant/agent in, no turn is ever stored.
type TrajectoryModule struct {
	bus   *ipc.Bus
	store *TrajectoryStore

	mu        sync.RWMutex
	recording map[string]bool // "tenant/agent" or "tenant/*" -> on

	logf func(string, map[string]any)
}

// TrajectoryOption configures the module.
type TrajectoryOption func(*TrajectoryModule)

// WithTrajectoryLogger attaches a structured logger.
func WithTrajectoryLogger(fn func(string, map[string]any)) TrajectoryOption {
	return func(m *TrajectoryModule) { m.logf = fn }
}

// WithRecordingDefault opts the given (tenant, agent) keys in at construction.
// Each key is "tenant/agent" or "tenant/*". Provided for deployments that want
// always-on recording for a specific tenant; the zero-config default is OFF.
func WithRecordingDefault(keys ...string) TrajectoryOption {
	return func(m *TrajectoryModule) {
		for _, k := range keys {
			m.recording[k] = true
		}
	}
}

// NewTrajectoryModule constructs the module over an open store.
func NewTrajectoryModule(store *TrajectoryStore, opts ...TrajectoryOption) *TrajectoryModule {
	m := &TrajectoryModule{store: store, recording: make(map[string]bool)}
	for _, o := range opts {
		o(m)
	}
	return m
}

func (m *TrajectoryModule) Name() string           { return "trajectory" }
func (m *TrajectoryModule) Dependencies() []string { return []string{"agent_runtime"} }

func (m *TrajectoryModule) Init(ctx context.Context, svc *kernel.Services) error {
	m.bus = svc.Bus
	return nil
}

func (m *TrajectoryModule) Start(ctx context.Context) error {
	if m.store == nil {
		m.log("trajectory store not configured; capture disabled", nil)
		return nil
	}
	// Live capture: subscribe to the existing turn-completed event. The handler
	// no-ops unless the turn's (tenant, agent) is opted in, so recording is OFF
	// by default and adds no storage cost to non-recording tenants.
	m.bus.Subscribe("trajectory", agent.TopicTurnCompleted, m.handleTurnCompleted)
	// Request/response control + ingest + export topics.
	m.bus.Handle("trajectory", TopicTrajectoryRecord, m.handleRecord)
	m.bus.Handle("trajectory", TopicTrajectoryExport, m.handleExport)
	m.bus.Handle("trajectory", TopicTrajectorySetRecording, m.handleSetRecording)
	return nil
}

func (m *TrajectoryModule) Stop(ctx context.Context) error {
	if m.store != nil {
		return m.store.Close()
	}
	return nil
}

func (m *TrajectoryModule) Health(ctx context.Context) kernel.HealthStatus {
	m.mu.RLock()
	n := len(m.recording)
	m.mu.RUnlock()
	return kernel.HealthStatus{Healthy: true, Message: fmt.Sprintf("%d recording keys", n)}
}

// SetRecording opts a (tenant, agent) in or out of live capture. agent "" sets
// the tenant-wide wildcard. Exported so in-process wiring can flip it too.
func (m *TrajectoryModule) SetRecording(tenant, agentName string, on bool) {
	if tenant == "" {
		return
	}
	key := recordingKey(tenant, agentName)
	m.mu.Lock()
	if on {
		m.recording[key] = true
	} else {
		delete(m.recording, key)
	}
	m.mu.Unlock()
}

// isRecording reports whether (tenant, agent) is opted in, honoring the
// tenant-wide wildcard. Default (nothing set) is false.
func (m *TrajectoryModule) isRecording(tenant, agentName string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.recording[recordingKey(tenant, "*")] || m.recording[recordingKey(tenant, agentName)]
}

func recordingKey(tenant, agentName string) string {
	if agentName == "" {
		agentName = "*"
	}
	return tenant + "/" + agentName
}

// handleTurnCompleted persists a trajectory for an opted-in turn. Event
// subscriber: errors are logged, never returned. Recording is OFF by default,
// so the common path is a single map lookup and an early return.
func (m *TrajectoryModule) handleTurnCompleted(msg ipc.Message) (ipc.Message, error) {
	rec, ok := msg.Payload.(agent.TurnRecord)
	if !ok {
		return ipc.Message{}, nil
	}
	if !m.isRecording(rec.Tenant, rec.Agent) {
		return ipc.Message{}, nil // recording off for this tenant/agent
	}
	t := trajectoryFromTurn(rec)
	if err := m.store.Insert(t); err != nil {
		m.log("trajectory: insert from turn failed", map[string]any{"error": err.Error(), "tenant": rec.Tenant})
	}
	return ipc.Message{}, nil
}

// trajectoryFromTurn maps a runtime TurnRecord onto a Trajectory. The event
// carries the ordered tool names (the decision sequence), the prompt and the
// final output, but not per-tool I/O — those steps land with empty
// observations. The store sanitizes all free text on the way in.
func trajectoryFromTurn(rec agent.TurnRecord) Trajectory {
	steps := make([]TrajectoryStep, 0, len(rec.ToolsUsed))
	for i, tool := range rec.ToolsUsed {
		steps = append(steps, newTrajectoryStep(i, tool))
	}
	return Trajectory{
		Schema:    TrajectorySchemaRaw,
		Tenant:    rec.Tenant,
		User:      rec.User,
		Agent:     rec.Agent,
		Session:   rec.Session,
		Model:     rec.Model,
		Prompt:    rec.UserMessage,
		Steps:     steps,
		Output:    rec.Response,
		Outcome:   rec.Outcome,
		ToolCalls: rec.ToolCalls,
		Turns:     rec.Turns,
		Subagent:  rec.Subagent,
		CreatedAt: time.Now(),
	}
}

func (m *TrajectoryModule) handleRecord(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(RecordRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected RecordRequest, got %T", msg.Payload)
	}
	t := req.Trajectory
	if t.Tenant == "" || t.Agent == "" {
		return ipc.Message{}, fmt.Errorf("trajectory.record: tenant and agent are required")
	}
	if !req.Force && !m.isRecording(t.Tenant, t.Agent) {
		// Honor opt-in for non-forced producers: a stray record call cannot
		// turn capture on for a tenant that never opted in.
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: map[string]string{"status": "skipped"}}, nil
	}
	if err := m.store.Insert(t); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: map[string]string{"status": "ok", "id": t.ID}}, nil
}

func (m *TrajectoryModule) handleExport(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(ExportRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected ExportRequest, got %T", msg.Payload)
	}
	if req.Tenant == "" {
		return ipc.Message{}, fmt.Errorf("trajectory.export: tenant is required")
	}
	trajs, err := m.store.List(req.Tenant, req.Agent, req.RunID, req.Limit)
	if err != nil {
		return ipc.Message{}, err
	}
	var sb strings.Builder
	if err := WriteJSONL(&sb, trajs); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: sb.String()}, nil
}

func (m *TrajectoryModule) handleSetRecording(msg ipc.Message) (ipc.Message, error) {
	req, ok := msg.Payload.(SetRecordingRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected SetRecordingRequest, got %T", msg.Payload)
	}
	if req.Tenant == "" {
		return ipc.Message{}, fmt.Errorf("trajectory.set_recording: tenant is required")
	}
	m.SetRecording(req.Tenant, req.Agent, req.On)
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: map[string]any{"tenant": req.Tenant, "agent": req.Agent, "on": req.On}}, nil
}

// WriteJSONL writes one JSON object per line for each trajectory (JSONL). Used
// for export both over IPC and from the CLI batch runner.
func WriteJSONL(w io.Writer, trajs []Trajectory) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, t := range trajs {
		if err := enc.Encode(t); err != nil {
			return fmt.Errorf("trajectory: jsonl encode: %w", err)
		}
	}
	return nil
}

func (m *TrajectoryModule) log(msg string, fields map[string]any) {
	if m.logf != nil {
		m.logf(msg, fields)
	}
}

func genTrajID() string {
	buf := make([]byte, 12)
	rand.Read(buf)
	return "traj_" + hex.EncodeToString(buf)
}
