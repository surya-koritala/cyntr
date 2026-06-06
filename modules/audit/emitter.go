package audit

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// Emitter simplifies publishing audit events to the IPC bus.
type Emitter struct {
	bus      *ipc.Bus
	instance string
}

// NewEmitter creates an audit event emitter.
func NewEmitter(bus *ipc.Bus, instance string) *Emitter {
	return &Emitter{bus: bus, instance: instance}
}

// Emit publishes an audit entry to the audit.write topic.
func (e *Emitter) Emit(entry Entry) {
	entry.Instance = e.instance
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if entry.ID == "" {
		entry.ID = "evt_" + time.Now().UTC().Format("20060102150405.000") + "_" + randomSuffix()
	}

	e.bus.Publish(ipc.Message{
		Source:  entry.Action.Module,
		Target:  "*",
		Type:    ipc.MessageTypeEvent,
		Topic:   "audit.write",
		Payload: entry,
	})
}

// randomSuffix returns a short cryptographically random hex string used to keep
// audit entry IDs unique even when many entries share the same millisecond
// timestamp (otherwise colliding primary keys silently drop records).
func randomSuffix() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to nanosecond entropy; rand.Read effectively never fails.
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(b[:])
}

// EmitPolicyCheck creates and publishes an audit entry for a policy decision.
func (e *Emitter) EmitPolicyCheck(tenant, user, agent, action, tool, rule, decision string, durationMs int) {
	e.Emit(Entry{
		Tenant:    tenant,
		Principal: Principal{User: user, Agent: agent},
		Action:    Action{Type: "policy_check", Module: "proxy", Detail: map[string]string{"action": action, "tool": tool}},
		Policy:    PolicyDecision{Rule: rule, Decision: decision, DecidedBy: "policy_engine", EvaluationMs: durationMs},
		Result:    Result{Status: decision},
	})
}

// EmitAgentChat creates and publishes an audit entry for an agent chat.
func (e *Emitter) EmitAgentChat(tenant, user, agent, message string, toolsUsed []string, durationMs int) {
	detail := map[string]string{"message_length": strconv.Itoa(len(message))}
	if len(toolsUsed) > 0 {
		detail["tools"] = toolsUsed[0] // first tool for indexing
	}
	e.Emit(Entry{
		Tenant:    tenant,
		Principal: Principal{User: user, Agent: agent},
		Action:    Action{Type: "agent_chat", Module: "agent_runtime", Detail: detail},
		Result:    Result{Status: "success", DurationMs: durationMs},
	})
}

// EmitAuth creates and publishes an audit entry for an auth event.
func (e *Emitter) EmitAuth(tenant, user, eventType, result string) {
	e.Emit(Entry{
		Tenant:    tenant,
		Principal: Principal{User: user},
		Action:    Action{Type: eventType, Module: "auth"},
		Result:    Result{Status: result},
	})
}
