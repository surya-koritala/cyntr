package trajectory

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/jobs"
	"github.com/cyntr-dev/cyntr/modules/eval"
)

func newQueue(t *testing.T) *jobs.Queue {
	t.Helper()
	q, err := jobs.NewQueue(filepath.Join(t.TempDir(), "jobs.db"),
		jobs.WithBackoff(func(int) time.Duration { return 0 }))
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	t.Cleanup(func() { q.Close() })
	return q
}

// mockRun returns a deterministic trajectory per sample, so the batch is
// reproducible. It mimics the isolated-subagent path (fresh run per call).
func mockRun(ctx context.Context, s Sample) (eval.Trajectory, error) {
	return eval.Trajectory{
		Tenant: s.Tenant,
		Agent:  s.Agent,
		Prompt: s.Prompt,
		Steps:  []eval.TrajectoryStep{{Index: 0, Tool: "http"}},
		Output: fmt.Sprintf("run %d done", s.Index),
	}, nil
}

func TestBatchNYieldsNRecords(t *testing.T) {
	q := newQueue(t)
	runner := NewRunner(q, mockRun, nil)
	trajs, err := runner.Run(context.Background(), Sample{Tenant: "acme", Agent: "assistant", Prompt: "go"}, 5)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(trajs) != 5 {
		t.Fatalf("expected 5 trajectories, got %d", len(trajs))
	}

	var buf bytes.Buffer
	if err := ExportJSONL(&buf, trajs); err != nil {
		t.Fatalf("ExportJSONL: %v", err)
	}
	lines := strings.Count(strings.TrimSpace(buf.String()), "\n") + 1
	if lines != 5 {
		t.Fatalf("expected 5 JSONL lines, got %d", lines)
	}
}

func TestBatchTagsRunAndTenant(t *testing.T) {
	q := newQueue(t)
	store, err := eval.NewTrajectoryStore(filepath.Join(t.TempDir(), "traj.db"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()

	runner := NewRunner(q, mockRun, store)
	base := Sample{Tenant: "acme", Agent: "assistant", Suite: "smoke", RunID: "run-1", Prompt: "go"}
	trajs, err := runner.Run(context.Background(), base, 3)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, tr := range trajs {
		if tr.Tenant != "acme" || tr.Suite != "smoke" || tr.RunID != "run-1" {
			t.Fatalf("run linkage not forced from sample: %+v", tr)
		}
	}
	// Persisted rows are tenant + run scoped.
	n, _ := store.Count("acme", "run-1")
	if n != 3 {
		t.Fatalf("expected 3 persisted rows for run-1, got %d", n)
	}
	other, _ := store.Count("globex", "run-1")
	if other != 0 {
		t.Fatalf("tenant isolation: globex should see 0 rows, got %d", other)
	}
}

func TestBatchRetriesOnError(t *testing.T) {
	q := newQueue(t)
	var calls int32
	flaky := func(ctx context.Context, s Sample) (eval.Trajectory, error) {
		// Fail the first attempt of index 0, then succeed.
		if s.Index == 0 && atomic.AddInt32(&calls, 1) == 1 {
			return eval.Trajectory{}, fmt.Errorf("transient")
		}
		return mockRun(ctx, s)
	}
	runner := NewRunner(q, flaky, nil)
	trajs, err := runner.Run(context.Background(), Sample{Tenant: "acme", Agent: "a", Prompt: "go"}, 2)
	if err != nil {
		t.Fatalf("Run should recover via retry: %v", err)
	}
	if len(trajs) != 2 {
		t.Fatalf("expected 2 trajectories after retry, got %d", len(trajs))
	}
}

func TestRunRejectsBadArgs(t *testing.T) {
	q := newQueue(t)
	runner := NewRunner(q, mockRun, nil)
	if _, err := runner.Run(context.Background(), Sample{Agent: "a", Prompt: "x"}, 1); err == nil {
		t.Fatal("missing tenant should error")
	}
	if _, err := runner.Run(context.Background(), Sample{Tenant: "acme", Agent: "a", Prompt: "x"}, 0); err == nil {
		t.Fatal("n=0 should error")
	}
}
