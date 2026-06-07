package eval

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// CompactStep is one element of the compact decision sequence. It preserves the
// ORDER and the tool chosen at each step (the decision), but normalizes I/O:
// large observations are referenced by a content hash into the trajectory's
// blob table rather than inlined, and repeated identical I/O is deduped to a
// single ref. The tool name (with any status suffix) is always preserved so the
// decision sequence is reconstructable.
type CompactStep struct {
	Index  int    `json:"i"`
	Tool   string `json:"t"`
	InRef  string `json:"in,omitempty"`  // hash ref into Blobs, or "" if no input
	OutRef string `json:"out,omitempty"` // hash ref into Blobs, or "" if no observation
}

// CompactTrajectory is the canonical compact form emitted by `trajectory
// compress`. The decision sequence (Steps, in order, with tool names) is
// preserved exactly; bulky/duplicated tool I/O is moved into the shared Blobs
// map keyed by content hash. Round-trips: Expand reconstructs a Trajectory with
// the same decision sequence and inlined I/O.
type CompactTrajectory struct {
	ID        string            `json:"id"`
	Schema    string            `json:"schema"`
	Tenant    string            `json:"tenant"`
	Agent     string            `json:"agent"`
	Session   string            `json:"session,omitempty"`
	Model     string            `json:"model,omitempty"`
	Suite     string            `json:"suite,omitempty"`
	RunID     string            `json:"run_id,omitempty"`
	Prompt    string            `json:"prompt"`
	Steps     []CompactStep     `json:"steps"`
	Output    string            `json:"output"`
	Outcome   string            `json:"outcome,omitempty"`
	ToolCalls int               `json:"tool_calls,omitempty"`
	Turns     int               `json:"turns,omitempty"`
	Blobs     map[string]string `json:"blobs"` // hash -> normalized, secret-stripped content
}

// Compress transforms one raw Trajectory into its compact canonical form. It:
//   - re-applies secret/PII redaction defensively (the offline input may be a
//     hand-edited or older-schema file that was never scrubbed),
//   - normalizes whitespace in tool I/O,
//   - dedupes identical I/O across steps into a single content-addressed blob,
//   - preserves the ordered decision sequence (one CompactStep per input step,
//     same tool names, same order).
func Compress(t Trajectory) CompactTrajectory {
	scrub := func(s string) string {
		return normalizeWhitespace(agent.RedactPII(agent.MaskSecrets(s)))
	}

	ct := CompactTrajectory{
		ID:        t.ID,
		Schema:    TrajectorySchemaCompact,
		Tenant:    t.Tenant,
		Agent:     t.Agent,
		Session:   t.Session,
		Model:     t.Model,
		Suite:     t.Suite,
		RunID:     t.RunID,
		Prompt:    scrub(t.Prompt),
		Output:    scrub(t.Output),
		Outcome:   t.Outcome,
		ToolCalls: t.ToolCalls,
		Turns:     t.Turns,
		Blobs:     map[string]string{},
	}

	intern := func(s string) string {
		s = scrub(s)
		if s == "" {
			return ""
		}
		h := blobHash(s)
		ct.Blobs[h] = s // dedupe: identical content collapses to one entry
		return h
	}

	ct.Steps = make([]CompactStep, 0, len(t.Steps))
	for _, st := range t.Steps {
		ct.Steps = append(ct.Steps, CompactStep{
			Index:  st.Index,
			Tool:   st.Tool,
			InRef:  intern(st.Input),
			OutRef: intern(st.Observation),
		})
	}
	return ct
}

// Expand reconstructs a Trajectory from its compact form by resolving blob
// references. The decision sequence (tool order + names) round-trips exactly;
// I/O is restored to its normalized, secret-stripped value (compression is
// lossy only with respect to whitespace and already-redacted secrets).
func Expand(ct CompactTrajectory) Trajectory {
	t := Trajectory{
		ID:        ct.ID,
		Schema:    TrajectorySchemaRaw,
		Tenant:    ct.Tenant,
		Agent:     ct.Agent,
		Session:   ct.Session,
		Model:     ct.Model,
		Suite:     ct.Suite,
		RunID:     ct.RunID,
		Prompt:    ct.Prompt,
		Output:    ct.Output,
		Outcome:   ct.Outcome,
		ToolCalls: ct.ToolCalls,
		Turns:     ct.Turns,
	}
	t.Steps = make([]TrajectoryStep, 0, len(ct.Steps))
	for _, cs := range ct.Steps {
		t.Steps = append(t.Steps, TrajectoryStep{
			Index:       cs.Index,
			Tool:        cs.Tool,
			Input:       ct.Blobs[cs.InRef],
			Observation: ct.Blobs[cs.OutRef],
		})
	}
	return t
}

// CompressStream reads raw trajectory JSONL from r, compresses each record, and
// writes compact JSONL to w. This is the offline transform behind `trajectory
// compress` (G29): a pure stream-to-stream function over G28 output, no store
// or network involved. Returns the number of records processed.
func CompressStream(r io.Reader, w io.Writer) (int, error) {
	sc := bufio.NewScanner(r)
	// Trajectories with large tool I/O can exceed the default 64K line cap.
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var t Trajectory
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			return n, fmt.Errorf("compress: line %d: %w", n+1, err)
		}
		if err := enc.Encode(Compress(t)); err != nil {
			return n, fmt.Errorf("compress: encode line %d: %w", n+1, err)
		}
		n++
	}
	if err := sc.Err(); err != nil {
		return n, fmt.Errorf("compress: read: %w", err)
	}
	return n, nil
}

// blobHash is a short, stable content address for a piece of tool I/O.
func blobHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8]) // 16 hex chars: collision-safe at this scale
}

// normalizeWhitespace collapses runs of whitespace to single spaces and trims
// the ends, so trivially-different observations (extra blank lines, trailing
// spaces) dedupe to the same blob.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
