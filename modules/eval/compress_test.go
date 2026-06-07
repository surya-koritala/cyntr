package eval

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func bigTraj() Trajectory {
	// Two steps share an identical large observation so dedupe has something to
	// collapse; the prompt carries a secret + PII to prove stripping.
	bigObs := strings.Repeat("the same large observation body. ", 200)
	return Trajectory{
		ID:     "traj_1",
		Schema: TrajectorySchemaRaw,
		Tenant: "acme",
		Agent:  "assistant",
		Prompt: "fetch data; my key is AKIAIOSFODNN7EXAMPLE; email me at carol@example.com",
		Steps: []TrajectoryStep{
			{Index: 0, Tool: "http", Input: "url=https://api.example.com", Observation: bigObs},
			{Index: 1, Tool: "json_query", Input: "q=.items", Observation: bigObs},
			{Index: 2, Tool: "http(denied)", Input: "", Observation: ""},
		},
		Output:    "here are the results",
		Outcome:   "ok",
		ToolCalls: 3,
		Turns:     4,
	}
}

func TestCompressPreservesDecisionSequence(t *testing.T) {
	ct := Compress(bigTraj())
	if len(ct.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(ct.Steps))
	}
	wantTools := []string{"http", "json_query", "http(denied)"}
	for i, st := range ct.Steps {
		if st.Tool != wantTools[i] {
			t.Fatalf("step %d tool = %q, want %q", i, st.Tool, wantTools[i])
		}
		if st.Index != i {
			t.Fatalf("step %d index = %d", i, st.Index)
		}
	}
	if ct.Schema != TrajectorySchemaCompact {
		t.Fatalf("schema = %q", ct.Schema)
	}
}

func TestCompressDedupesAndShrinks(t *testing.T) {
	tr := bigTraj()
	raw, _ := json.Marshal(tr)
	ct := Compress(tr)
	compact, _ := json.Marshal(ct)

	if len(compact) >= len(raw) {
		t.Fatalf("compress did not shrink: raw=%d compact=%d", len(raw), len(compact))
	}
	// The two identical observations must collapse to one blob (plus the two
	// distinct inputs = 3 blobs total; empty I/O is not interned).
	if len(ct.Blobs) != 3 {
		t.Fatalf("expected 3 deduped blobs, got %d: %v", len(ct.Blobs), keys(ct.Blobs))
	}
	if ct.Steps[0].OutRef == "" || ct.Steps[0].OutRef != ct.Steps[1].OutRef {
		t.Fatalf("identical observations should share one blob ref: %q vs %q", ct.Steps[0].OutRef, ct.Steps[1].OutRef)
	}
}

func TestCompressStripsSecretsAndPII(t *testing.T) {
	ct := Compress(bigTraj())
	if strings.Contains(ct.Prompt, "AKIA") || strings.Contains(ct.Prompt, "carol@example.com") {
		t.Fatalf("secret/PII survived compression: %q", ct.Prompt)
	}
	blob, _ := json.Marshal(ct.Blobs)
	if strings.Contains(string(blob), "AKIA") {
		t.Fatal("secret survived in a blob")
	}
}

func TestCompressRoundTripsToSchema(t *testing.T) {
	tr := bigTraj()
	ct := Compress(tr)
	back := Expand(ct)

	if back.Schema != TrajectorySchemaRaw {
		t.Fatalf("expanded schema = %q", back.Schema)
	}
	if len(back.Steps) != len(tr.Steps) {
		t.Fatalf("step count changed on round-trip: %d -> %d", len(tr.Steps), len(back.Steps))
	}
	for i := range tr.Steps {
		if back.Steps[i].Tool != tr.Steps[i].Tool {
			t.Fatalf("step %d tool not preserved: %q -> %q", i, tr.Steps[i].Tool, back.Steps[i].Tool)
		}
	}
	// The shared observation must be restored to both steps from the single blob.
	if back.Steps[0].Observation == "" || back.Steps[0].Observation != back.Steps[1].Observation {
		t.Fatal("observation not restored from shared blob on expand")
	}
	if back.Output == "" || back.ToolCalls != tr.ToolCalls || back.Turns != tr.Turns {
		t.Fatalf("scalar fields not preserved: %+v", back)
	}
}

func TestCompressStream(t *testing.T) {
	var in bytes.Buffer
	enc := json.NewEncoder(&in)
	for i := 0; i < 3; i++ {
		enc.Encode(bigTraj())
	}

	var out bytes.Buffer
	n, err := CompressStream(&in, &out)
	if err != nil {
		t.Fatalf("CompressStream: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 records, got %d", n)
	}
	lines := strings.Count(strings.TrimSpace(out.String()), "\n") + 1
	if lines != 3 {
		t.Fatalf("expected 3 JSONL lines, got %d", lines)
	}
	// Each output line must be a valid CompactTrajectory.
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var ct CompactTrajectory
		if err := json.Unmarshal([]byte(line), &ct); err != nil {
			t.Fatalf("output line not a CompactTrajectory: %v", err)
		}
		if ct.Schema != TrajectorySchemaCompact {
			t.Fatalf("line schema = %q", ct.Schema)
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
