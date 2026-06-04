package jobs

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestQueue(t *testing.T, opts ...Option) *Queue {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jobs.db")
	// Default to zero backoff so retries are immediately re-eligible under
	// RunOnce; individual tests override as needed.
	base := []Option{WithBackoff(func(int) time.Duration { return 0 })}
	q, err := NewQueue(path, append(base, opts...)...)
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	t.Cleanup(func() { q.Close() })
	return q
}

func TestEnqueueRunsExactlyOnce(t *testing.T) {
	q := newTestQueue(t)
	var calls atomic.Int32
	var gotPayload atomic.Value
	q.Register("greet", func(ctx context.Context, j Job) error {
		calls.Add(1)
		gotPayload.Store(string(j.Payload))
		return nil
	})

	id, err := q.Enqueue("acme", "greet", []byte("hello"), time.Time{})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	q.RunOnce(context.Background())
	q.RunOnce(context.Background()) // second pass must not re-run a done job

	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1", calls.Load())
	}
	if gotPayload.Load() != "hello" {
		t.Fatalf("payload = %v, want hello", gotPayload.Load())
	}
	job, err := q.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if job.State != StateDone {
		t.Fatalf("state = %q, want done", job.State)
	}
}

func TestJobNotDueIsSkipped(t *testing.T) {
	q := newTestQueue(t)
	var calls atomic.Int32
	q.Register("later", func(ctx context.Context, j Job) error { calls.Add(1); return nil })

	id, _ := q.Enqueue("acme", "later", nil, time.Now().Add(time.Hour))
	q.RunOnce(context.Background())

	if calls.Load() != 0 {
		t.Fatalf("future job ran early")
	}
	job, _ := q.Get(id)
	if job.State != StatePending {
		t.Fatalf("state = %q, want pending", job.State)
	}
}

func TestUnregisteredKindLeftPending(t *testing.T) {
	q := newTestQueue(t)
	id, _ := q.Enqueue("acme", "unknown", nil, time.Time{})

	q.RunOnce(context.Background())
	if job, _ := q.Get(id); job.State != StatePending {
		t.Fatalf("unregistered-kind job state = %q, want pending (not stuck running/failed)", job.State)
	}

	// Registering the handler later lets it run.
	var ran atomic.Int32
	q.Register("unknown", func(ctx context.Context, j Job) error { ran.Add(1); return nil })
	q.RunOnce(context.Background())
	if ran.Load() != 1 {
		t.Fatalf("job did not run after handler registered")
	}
}

func TestFailureRetriesThenFails(t *testing.T) {
	q := newTestQueue(t, WithMaxAttempts(3))
	var calls atomic.Int32
	q.Register("boom", func(ctx context.Context, j Job) error {
		calls.Add(1)
		return context.DeadlineExceeded // any error
	})
	id, _ := q.Enqueue("acme", "boom", nil, time.Time{})

	// Three passes: attempts 1, 2, then 3 -> failed.
	for i := 0; i < 3; i++ {
		q.RunOnce(context.Background())
	}
	job, _ := q.Get(id)
	if job.State != StateFailed {
		t.Fatalf("state = %q, want failed", job.State)
	}
	if job.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3", job.Attempts)
	}
	if job.LastError == "" {
		t.Fatalf("expected last_error to be recorded")
	}
	if calls.Load() != 3 {
		t.Fatalf("handler ran %d times, want 3", calls.Load())
	}

	// A failed job is terminal — further passes do nothing.
	q.RunOnce(context.Background())
	if calls.Load() != 3 {
		t.Fatalf("failed job ran again")
	}
}

func TestSucceedsAfterTransientFailures(t *testing.T) {
	q := newTestQueue(t, WithMaxAttempts(5))
	var calls atomic.Int32
	q.Register("flaky", func(ctx context.Context, j Job) error {
		if calls.Add(1) < 3 {
			return context.DeadlineExceeded
		}
		return nil
	})
	id, _ := q.Enqueue("acme", "flaky", nil, time.Time{})
	for i := 0; i < 3; i++ {
		q.RunOnce(context.Background())
	}
	job, _ := q.Get(id)
	if job.State != StateDone {
		t.Fatalf("state = %q, want done", job.State)
	}
	if calls.Load() != 3 {
		t.Fatalf("handler ran %d times, want 3", calls.Load())
	}
}

func TestPanickingHandlerIsContainedAndRetried(t *testing.T) {
	q := newTestQueue(t, WithMaxAttempts(2))
	q.Register("panic", func(ctx context.Context, j Job) error { panic("boom") })
	id, _ := q.Enqueue("acme", "panic", nil, time.Time{})

	q.RunOnce(context.Background()) // attempt 1 (panic -> error)
	if job, _ := q.Get(id); job.State != StatePending {
		t.Fatalf("after first panic state = %q, want pending", job.State)
	}
	q.RunOnce(context.Background()) // attempt 2 -> failed
	job, _ := q.Get(id)
	if job.State != StateFailed {
		t.Fatalf("state = %q, want failed", job.State)
	}
	if job.LastError == "" {
		t.Fatalf("panic should be recorded as last_error")
	}
}

func TestSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.db")

	q1, err := NewQueue(path, WithBackoff(func(int) time.Duration { return 0 }))
	if err != nil {
		t.Fatalf("NewQueue: %v", err)
	}
	id, _ := q1.Enqueue("acme", "persist", []byte("x"), time.Time{})
	q1.Close() // simulate shutdown before the job ran

	q2, err := NewQueue(path, WithBackoff(func(int) time.Duration { return 0 }))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer q2.Close()
	var ran atomic.Int32
	q2.Register("persist", func(ctx context.Context, j Job) error { ran.Add(1); return nil })
	q2.RunOnce(context.Background())

	if ran.Load() != 1 {
		t.Fatalf("persisted job did not run after restart")
	}
	if job, _ := q2.Get(id); job.State != StateDone {
		t.Fatalf("state = %q, want done", job.State)
	}
}

func TestRecoversStaleRunningJobs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.db")

	q1, _ := NewQueue(path)
	id, _ := q1.Enqueue("acme", "stuck", nil, time.Time{})
	q1.Close()

	// Simulate a crash mid-run: force the row into the running state.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(`UPDATE jobs SET state=? WHERE id=?`, StateRunning, id); err != nil {
		t.Fatalf("force running: %v", err)
	}
	raw.Close()

	// Reopening must recover running -> pending so the job runs again.
	q2, _ := NewQueue(path, WithBackoff(func(int) time.Duration { return 0 }))
	defer q2.Close()
	var ran atomic.Int32
	q2.Register("stuck", func(ctx context.Context, j Job) error { ran.Add(1); return nil })
	q2.RunOnce(context.Background())

	if ran.Load() != 1 {
		t.Fatalf("stale running job was not recovered")
	}
}

func TestPerTenantConcurrencyCap(t *testing.T) {
	q := newTestQueue(t, WithPerTenantLimit(1), WithGlobalLimit(8), WithPollInterval(5*time.Millisecond))

	var active, maxActive atomic.Int32
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	q.Register("block", func(ctx context.Context, j Job) error {
		n := active.Add(1)
		for {
			m := maxActive.Load()
			if n <= m || maxActive.CompareAndSwap(m, n) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		active.Add(-1)
		return nil
	})

	// Two jobs for the SAME tenant; the cap of 1 must serialize them.
	q.Enqueue("acme", "block", nil, time.Time{})
	q.Enqueue("acme", "block", nil, time.Time{})
	q.Start()

	// First job should enter; the second must not while the first is blocked.
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first job never started")
	}
	select {
	case <-entered:
		t.Fatal("second same-tenant job ran while the first held the only slot")
	case <-time.After(150 * time.Millisecond):
	}
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("max concurrent same-tenant jobs = %d, want 1", got)
	}

	// Release the first; the second should now run, and both reach done.
	close(release)
	deadline := time.After(2 * time.Second)
	for {
		done, _ := q.CountByState("acme", StateDone)
		if done == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("jobs did not both complete (done=%d)", done)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestDifferentTenantsRunConcurrently(t *testing.T) {
	q := newTestQueue(t, WithPerTenantLimit(1), WithGlobalLimit(8), WithPollInterval(5*time.Millisecond))

	var active, maxActive atomic.Int32
	bothIn := make(chan struct{})
	release := make(chan struct{})
	var reached int32
	q.Register("block", func(ctx context.Context, j Job) error {
		n := active.Add(1)
		for {
			m := maxActive.Load()
			if n <= m || maxActive.CompareAndSwap(m, n) {
				break
			}
		}
		if atomic.AddInt32(&reached, 1) == 2 {
			close(bothIn)
		}
		<-release
		active.Add(-1)
		return nil
	})

	// One job each for two tenants — per-tenant cap of 1 does not prevent
	// cross-tenant parallelism.
	q.Enqueue("acme", "block", nil, time.Time{})
	q.Enqueue("globex", "block", nil, time.Time{})
	q.Start()

	select {
	case <-bothIn:
	case <-time.After(time.Second):
		t.Fatalf("both tenants did not run concurrently (max=%d)", maxActive.Load())
	}
	if got := maxActive.Load(); got != 2 {
		t.Fatalf("max concurrent = %d, want 2", got)
	}
	close(release)
}

func TestEnqueueValidation(t *testing.T) {
	q := newTestQueue(t)
	if _, err := q.Enqueue("", "kind", nil, time.Time{}); err == nil {
		t.Fatal("expected error for empty tenant")
	}
	if _, err := q.Enqueue("acme", "", nil, time.Time{}); err == nil {
		t.Fatal("expected error for empty kind")
	}
}
