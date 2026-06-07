package eval

import "time"

// Trajectory schema version. The compress transform (G29) round-trips to this
// documented schema; bumping it is a breaking change to the canonical form.
const (
	TrajectorySchemaRaw       = "cyntr.trajectory/v1"
	TrajectorySchemaCompact   = "cyntr.trajectory.compact/v1"
	TrajectoryStepDecision    = "decision" // a model turn that chose tool(s)
	TrajectoryStepObservation = "observation"
)

// TrajectoryStep is one element of the decision sequence inside a trajectory:
// either a tool call (the agent's decision) and/or the observation that came
// back from it. The ordered slice of steps IS the decision sequence that the
// compress transform must preserve.
type TrajectoryStep struct {
	Index       int    `json:"index"`
	Tool        string `json:"tool"`                  // tool name (may carry a "(denied)"/"(timeout)" status suffix from the runtime)
	Input       string `json:"input,omitempty"`       // tool input, normalized/redacted
	Observation string `json:"observation,omitempty"` // tool output, normalized/redacted
}

// Trajectory is the FULL captured record of one agent turn: the prompt that
// started it, the ordered tool-call/observation decision sequence, and the
// final output. Tenant-scoped; persisted only when recording is opted in.
//
// Privacy: callers MUST run Prompt/Output/Steps through MaskSecrets+RedactPII
// before persistence. TrajectoryStore.Insert does this defensively, but the
// record is also sanitized at the source (the live recorder) before it lands.
type Trajectory struct {
	ID        string           `json:"id"`
	Schema    string           `json:"schema"`
	Tenant    string           `json:"tenant"`
	User      string           `json:"user"`
	Agent     string           `json:"agent"`
	Session   string           `json:"session"`
	Model     string           `json:"model"`
	Suite     string           `json:"suite,omitempty"` // batch/suite label for `trajectory run`
	RunID     string           `json:"run_id,omitempty"`
	Prompt    string           `json:"prompt"`
	Steps     []TrajectoryStep `json:"steps"`
	Output    string           `json:"output"`
	Outcome   string           `json:"outcome"`
	ToolCalls int              `json:"tool_calls"`
	Turns     int              `json:"turns"`
	Subagent  bool             `json:"subagent"`
	CreatedAt time.Time        `json:"created_at"`
}

// trajectoryFromTurn builds a Trajectory from an agent.TurnRecord. The runtime
// event carries the ordered tool names (the decision sequence) plus the prompt
// and final output; per-tool I/O is not on the event, so the live-captured
// trajectory records the decision sequence with empty observations. The
// `trajectory run` fan-out path records full I/O directly via RecordRequest.
func newTrajectoryStep(i int, tool string) TrajectoryStep {
	return TrajectoryStep{Index: i, Tool: tool}
}
