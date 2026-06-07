// Package trajectory implements the `cyntr trajectory` CLI subtree: batch
// trajectory generation (run) and the offline compression transform (compress).
//
// The batch runner fans N agent runs out over the shared, durable jobs queue
// (kernel/jobs), each run going through the isolated-subagent path (a fresh,
// caller-tenant-scoped agent.chat, exactly like delegate/orchestrate spawn
// children). Each completed run is persisted as a full Trajectory and the batch
// is exported as JSONL. Tenant isolation is carried on every job and every row.
package trajectory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/jobs"
	"github.com/cyntr-dev/cyntr/modules/eval"
)

// JobKindTrajectoryRun is the queue job kind for one batch sample.
const JobKindTrajectoryRun = "trajectory.run"

// IsolatedRun executes one isolated agent run and returns the captured
// trajectory. Implementations route through the isolated-subagent path: a
// fresh session under the caller's tenant/user (the same contract delegate/
// orchestrate use when they spawn children). The runner injects this so it is
// testable with a mock and wired to the live bus / HTTP API in the CLI.
type IsolatedRun func(ctx context.Context, sample Sample) (eval.Trajectory, error)

// Sample is one unit of a batch: a prompt to run against an agent. Tenant and
// agent are mandatory so every resulting trajectory is tenant-scoped.
type Sample struct {
	Tenant string `json:"tenant"`
	Agent  string `json:"agent"`
	User   string `json:"user"`
	Suite  string `json:"suite"`
	RunID  string `json:"run_id"`
	Index  int    `json:"index"`
	Prompt string `json:"prompt"`
}

// Runner fans batch samples out over the jobs queue and collects the resulting
// trajectories. It owns no global state — every method is tenant-scoped via the
// samples it is given.
type Runner struct {
	queue *jobs.Queue
	run   IsolatedRun
	store *eval.TrajectoryStore // optional: persist as well as collect

	mu      sync.Mutex
	results map[int]eval.Trajectory // sample index -> latest trajectory (idempotent across retries)
}

// NewRunner builds a batch runner over a jobs queue and an isolated-run fn.
// store may be nil (collect-only); when set, each trajectory is also persisted
// tenant-scoped via the store.
func NewRunner(queue *jobs.Queue, run IsolatedRun, store *eval.TrajectoryStore) *Runner {
	return &Runner{queue: queue, run: run, store: store}
}

// Run fans n copies of the base sample out over the jobs queue, drives the
// queue to completion, and returns the collected trajectories in submission
// order. Reusing the jobs queue gives at-least-once delivery, per-tenant
// concurrency caps, and crash durability for free.
func (r *Runner) Run(ctx context.Context, base Sample, n int) ([]eval.Trajectory, error) {
	if n <= 0 {
		return nil, fmt.Errorf("trajectory run: n must be > 0")
	}
	if base.Tenant == "" || base.Agent == "" {
		return nil, fmt.Errorf("trajectory run: tenant and agent are required")
	}
	if base.RunID == "" {
		base.RunID = "trajrun_" + time.Now().UTC().Format("20060102T150405")
	}

	r.mu.Lock()
	r.results = make(map[int]eval.Trajectory)
	r.mu.Unlock()

	r.queue.Register(JobKindTrajectoryRun, r.handleJob)

	for i := 0; i < n; i++ {
		s := base
		s.Index = i
		payload, err := json.Marshal(s)
		if err != nil {
			return nil, fmt.Errorf("trajectory run: marshal sample: %w", err)
		}
		// Every job carries the tenant — the queue's per-tenant cap and our
		// row-level tenant column both depend on it.
		if _, err := r.queue.Enqueue(base.Tenant, JobKindTrajectoryRun, payload, time.Time{}); err != nil {
			return nil, fmt.Errorf("trajectory run: enqueue: %w", err)
		}
	}

	// Drive the queue until every sample has a result or an error. RunOnce
	// leases due jobs and waits for that pass; loop until none remain pending.
	deadline := time.Now().Add(10 * time.Minute)
	for {
		r.queue.RunOnce(ctx)
		pending, err := r.queue.CountByState(base.Tenant, jobs.StatePending)
		if err != nil {
			return nil, fmt.Errorf("trajectory run: count: %w", err)
		}
		running, _ := r.queue.CountByState(base.Tenant, jobs.StateRunning)
		if pending == 0 && running == 0 {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("trajectory run: timed out with %d pending", pending)
		}
	}

	// Collect results in submission order. A sample with no result exhausted its
	// retry budget (the queue marked it failed); report how many.
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]eval.Trajectory, 0, len(r.results))
	missing := 0
	for i := 0; i < n; i++ {
		tr, ok := r.results[i]
		if !ok {
			missing++
			continue
		}
		out = append(out, tr)
	}
	if missing > 0 {
		return out, fmt.Errorf("trajectory run: %d/%d samples failed after retries", missing, n)
	}
	return out, nil
}

// handleJob runs one sample through the isolated-subagent path and records the
// trajectory. Idempotency: the jobs queue may retry, so a run that already
// produced a trajectory for this index is harmless — we just collect it again
// and the store's per-id primary key dedupes persisted rows.
func (r *Runner) handleJob(ctx context.Context, job jobs.Job) error {
	var s Sample
	if err := json.Unmarshal(job.Payload, &s); err != nil {
		return fmt.Errorf("trajectory run: bad payload: %w", err)
	}
	traj, err := r.run(ctx, s)
	if err != nil {
		return err // let the queue retry per its backoff/attempt budget
	}
	// Force tenant/agent/suite/run linkage from the trusted sample, never the
	// returned trajectory, so a run can't be mislabeled into another tenant.
	traj.Tenant = s.Tenant
	traj.Agent = s.Agent
	if traj.User == "" {
		traj.User = s.User
	}
	traj.Suite = s.Suite
	traj.RunID = s.RunID

	if r.store != nil {
		if err := r.store.Insert(traj); err != nil {
			return fmt.Errorf("trajectory run: persist: %w", err)
		}
	}

	r.mu.Lock()
	r.results[s.Index] = traj
	r.mu.Unlock()
	return nil
}

// ExportJSONL writes the collected trajectories as JSONL to w.
func ExportJSONL(w io.Writer, trajs []eval.Trajectory) error {
	return eval.WriteJSONL(w, trajs)
}
