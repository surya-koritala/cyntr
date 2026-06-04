// Package jobs provides a small, durable, SQLite-backed background job queue.
//
// It is the shared "do this work later, off the hot path, with retry and
// per-tenant rate limiting" primitive that the learning loop, cross-session
// recall indexer, and trajectory capture build on, so each of those features
// does not reinvent the usermodel module's bespoke ticker.
//
// Design goals (Sprint 0 / F0.2):
//   - At-least-once delivery: a job runs until a handler returns nil or the
//     attempt budget is exhausted. Handlers must therefore be idempotent.
//   - Durable: jobs survive process restart (they live in SQLite). Jobs left
//     mid-flight by a crash are recovered to pending on the next start.
//   - Multi-tenant: every job carries a tenant, and a per-tenant concurrency
//     cap keeps one noisy tenant from starving the others.
//   - No busy-loop: a single ticker polls on an interval; it never spins.
//
// The queue is an in-process library (like the module stores), not an IPC
// module: consumers hold a *Queue and call Enqueue / Register directly.
package jobs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Job states.
const (
	StatePending = "pending"
	StateRunning = "running"
	StateDone    = "done"
	StateFailed  = "failed"
)

// Job is one unit of deferred work.
type Job struct {
	ID        string
	Tenant    string
	Kind      string
	Payload   []byte // opaque to the queue; handlers decode it
	RunAfter  time.Time
	Attempts  int
	State     string
	LastError string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Handler processes a job of a given kind. Returning nil marks the job done;
// returning an error reschedules it with backoff until the attempt budget is
// exhausted, after which the job is marked failed. A handler panic is treated
// as an error. Handlers MUST be idempotent — a job may run more than once
// across retries or crash recovery.
type Handler func(ctx context.Context, job Job) error

// Queue is a durable background job queue. A Queue is safe for concurrent use.
type Queue struct {
	mu       sync.Mutex // guards db writes, handlers, inflight, lifecycle
	db       *sql.DB
	handlers map[string]Handler
	inflight map[string]int // tenant -> currently running jobs

	maxAttempts  int
	perTenantCap int
	globalCap    int
	leaseBatch   int
	pollInterval time.Duration
	jobTimeout   time.Duration
	backoff      func(attempt int) time.Duration
	now          func() time.Time
	logf         func(string, map[string]any)

	wg      sync.WaitGroup
	stopCh  chan struct{}
	doneCh  chan struct{}
	started bool
}

// Option configures a Queue.
type Option func(*Queue)

// WithMaxAttempts sets how many times a job is tried before it is marked
// failed (default 5).
func WithMaxAttempts(n int) Option { return func(q *Queue) { q.maxAttempts = n } }

// WithPerTenantLimit caps how many jobs one tenant may run concurrently
// (default 4). This is the queue's per-tenant rate limit; callers that also
// want token/quota accounting should check the quota module at Enqueue time.
func WithPerTenantLimit(n int) Option { return func(q *Queue) { q.perTenantCap = n } }

// WithGlobalLimit caps total concurrent jobs across all tenants (default 8).
func WithGlobalLimit(n int) Option { return func(q *Queue) { q.globalCap = n } }

// WithPollInterval sets how often the ticker scans for due jobs (default 1s).
func WithPollInterval(d time.Duration) Option { return func(q *Queue) { q.pollInterval = d } }

// WithJobTimeout bounds a single handler invocation (default 0 = no timeout).
func WithJobTimeout(d time.Duration) Option { return func(q *Queue) { q.jobTimeout = d } }

// WithBackoff sets the retry delay as a function of the (1-based) attempt
// number. The default is min(60s, 2^attempt seconds).
func WithBackoff(fn func(attempt int) time.Duration) Option {
	return func(q *Queue) { q.backoff = fn }
}

// WithClock overrides the time source (for tests).
func WithClock(fn func() time.Time) Option { return func(q *Queue) { q.now = fn } }

// WithLogger attaches a structured logger.
func WithLogger(fn func(string, map[string]any)) Option { return func(q *Queue) { q.logf = fn } }

// NewQueue opens (or creates) a SQLite-backed queue at dbPath. Any jobs left
// in the running state by a previous process (a crash) are recovered to
// pending so they run again.
func NewQueue(dbPath string, opts ...Option) (*Queue, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open jobs db: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA busy_timeout=5000")

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS jobs (
		id         TEXT PRIMARY KEY,
		tenant     TEXT NOT NULL,
		kind       TEXT NOT NULL,
		payload    BLOB,
		run_after  TEXT NOT NULL,
		attempts   INTEGER NOT NULL DEFAULT 0,
		state      TEXT NOT NULL DEFAULT 'pending',
		last_error TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create jobs table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_jobs_due ON jobs(state, run_after)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create jobs index: %w", err)
	}

	q := &Queue{
		db:           db,
		handlers:     make(map[string]Handler),
		inflight:     make(map[string]int),
		maxAttempts:  5,
		perTenantCap: 4,
		globalCap:    8,
		leaseBatch:   100,
		pollInterval: time.Second,
		now:          time.Now,
		backoff:      defaultBackoff,
	}
	for _, o := range opts {
		o(q)
	}

	// Recover jobs that were running when the process last stopped.
	if _, err := db.Exec(`UPDATE jobs SET state=? WHERE state=?`, StatePending, StateRunning); err != nil {
		db.Close()
		return nil, fmt.Errorf("recover running jobs: %w", err)
	}
	return q, nil
}

func defaultBackoff(attempt int) time.Duration {
	d := time.Second << attempt // 2,4,8,... seconds
	if d > time.Minute || d <= 0 {
		return time.Minute
	}
	return d
}

// Register installs a handler for a job kind. Registering an unknown kind's
// handler after Start is fine; jobs of kinds with no handler are simply left
// pending until one is registered.
func (q *Queue) Register(kind string, h Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[kind] = h
}

// Enqueue persists a new job. runAfter is when the job first becomes eligible
// to run; pass the zero time (or a past time) to make it eligible immediately.
func (q *Queue) Enqueue(tenant, kind string, payload []byte, runAfter time.Time) (string, error) {
	if tenant == "" || kind == "" {
		return "", fmt.Errorf("jobs: tenant and kind are required")
	}
	now := q.now().UTC()
	if runAfter.IsZero() {
		runAfter = now
	}
	id := genID()
	q.mu.Lock()
	defer q.mu.Unlock()
	_, err := q.db.Exec(
		`INSERT INTO jobs (id, tenant, kind, payload, run_after, attempts, state, last_error, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 0, ?, '', ?, ?)`,
		id, tenant, kind, payload, runAfter.UTC().Format(time.RFC3339Nano), StatePending,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return "", fmt.Errorf("jobs: enqueue: %w", err)
	}
	return id, nil
}

// Start launches the background ticker. It is a no-op if already started.
func (q *Queue) Start() {
	q.mu.Lock()
	if q.started {
		q.mu.Unlock()
		return
	}
	q.started = true
	q.stopCh = make(chan struct{})
	q.doneCh = make(chan struct{})
	q.mu.Unlock()
	go q.loop()
}

func (q *Queue) loop() {
	defer close(q.doneCh)
	t := time.NewTicker(q.pollInterval)
	defer t.Stop()
	for {
		select {
		case <-q.stopCh:
			return
		case <-t.C:
			q.tick(context.Background())
		}
	}
}

// Stop halts the ticker and waits for in-flight handlers to drain. Safe to
// call when not started.
func (q *Queue) Stop() {
	q.mu.Lock()
	if !q.started {
		q.mu.Unlock()
		return
	}
	q.started = false
	close(q.stopCh)
	q.mu.Unlock()
	<-q.doneCh
	q.wg.Wait()
}

// Close stops the queue (if running) and closes the database.
func (q *Queue) Close() error {
	q.Stop()
	return q.db.Close()
}

// tick performs one lease-and-dispatch pass. It is exported-for-test via
// RunOnce; the background loop calls it on each poll.
func (q *Queue) tick(ctx context.Context) {
	q.mu.Lock()
	claimed := q.leaseLocked()
	q.mu.Unlock()
	for _, job := range claimed {
		q.wg.Add(1)
		go q.run(ctx, job)
	}
}

// RunOnce performs a single synchronous lease-and-dispatch pass and waits for
// the dispatched handlers of that pass to finish. It exists for deterministic
// testing and for callers that want to drive the queue manually instead of
// via the ticker.
func (q *Queue) RunOnce(ctx context.Context) {
	q.mu.Lock()
	claimed := q.leaseLocked()
	q.mu.Unlock()
	var wg sync.WaitGroup
	for _, job := range claimed {
		q.wg.Add(1)
		wg.Add(1)
		go func(j Job) {
			defer wg.Done()
			q.run(ctx, j)
		}(job)
	}
	wg.Wait()
}

// leaseLocked selects due jobs whose kind has a registered handler, respecting
// the global and per-tenant concurrency caps, atomically claims them
// (pending -> running), and returns the claimed jobs. Caller must hold q.mu.
func (q *Queue) leaseLocked() []Job {
	if len(q.handlers) == 0 {
		return nil
	}
	used := 0
	for _, c := range q.inflight {
		used += c
	}
	slots := q.globalCap - used
	if slots <= 0 {
		return nil
	}

	now := q.now().UTC()
	rows, err := q.db.Query(
		`SELECT id, tenant, kind, payload, run_after, attempts FROM jobs
		 WHERE state=? AND run_after<=? ORDER BY run_after ASC LIMIT ?`,
		StatePending, now.Format(time.RFC3339Nano), q.leaseBatch,
	)
	if err != nil {
		q.log("jobs: lease query failed", map[string]any{"error": err.Error()})
		return nil
	}
	// Scan all candidates before issuing UPDATEs — modernc.org/sqlite serves a
	// single connection, so reading and writing must not interleave.
	type cand struct {
		j        Job
		runAfter string
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.j.ID, &c.j.Tenant, &c.j.Kind, &c.j.Payload, &c.runAfter, &c.j.Attempts); err != nil {
			continue
		}
		cands = append(cands, c)
	}
	rows.Close()

	var claimed []Job
	for _, c := range cands {
		if slots <= 0 {
			break
		}
		if _, ok := q.handlers[c.j.Kind]; !ok {
			continue // no handler yet — leave pending
		}
		if q.inflight[c.j.Tenant] >= q.perTenantCap {
			continue // tenant is at its cap this round
		}
		res, err := q.db.Exec(
			`UPDATE jobs SET state=?, updated_at=? WHERE id=? AND state=?`,
			StateRunning, now.Format(time.RFC3339Nano), c.j.ID, StatePending,
		)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n != 1 {
			continue // lost the claim race
		}
		c.j.RunAfter, _ = parseTime(c.runAfter)
		c.j.State = StateRunning
		q.inflight[c.j.Tenant]++
		slots--
		claimed = append(claimed, c.j)
	}
	return claimed
}

// run executes a claimed job's handler and records the outcome.
func (q *Queue) run(ctx context.Context, job Job) {
	defer q.wg.Done()

	q.mu.Lock()
	h := q.handlers[job.Kind]
	q.mu.Unlock()

	hctx := ctx
	if q.jobTimeout > 0 {
		var cancel context.CancelFunc
		hctx, cancel = context.WithTimeout(ctx, q.jobTimeout)
		defer cancel()
	}

	herr := safeInvoke(h, hctx, job)

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.inflight[job.Tenant] > 0 {
		q.inflight[job.Tenant]--
		if q.inflight[job.Tenant] == 0 {
			delete(q.inflight, job.Tenant)
		}
	}

	now := q.now().UTC()
	if herr == nil {
		q.db.Exec(`UPDATE jobs SET state=?, updated_at=? WHERE id=?`,
			StateDone, now.Format(time.RFC3339Nano), job.ID)
		return
	}

	attempts := job.Attempts + 1
	if attempts >= q.maxAttempts {
		q.db.Exec(`UPDATE jobs SET state=?, attempts=?, last_error=?, updated_at=? WHERE id=?`,
			StateFailed, attempts, herr.Error(), now.Format(time.RFC3339Nano), job.ID)
		q.log("jobs: job failed permanently", map[string]any{
			"id": job.ID, "kind": job.Kind, "tenant": job.Tenant, "attempts": attempts, "error": herr.Error(),
		})
		return
	}
	next := now.Add(q.backoff(attempts))
	q.db.Exec(`UPDATE jobs SET state=?, attempts=?, last_error=?, run_after=?, updated_at=? WHERE id=?`,
		StatePending, attempts, herr.Error(), next.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), job.ID)
}

// safeInvoke runs a handler, converting a panic into an error so one bad job
// never takes down the queue.
func safeInvoke(h Handler, ctx context.Context, job Job) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("jobs: handler panicked: %v", r)
		}
	}()
	if h == nil {
		return fmt.Errorf("jobs: no handler for kind %q", job.Kind)
	}
	return h(ctx, job)
}

// Get returns the current persisted state of a job by ID.
func (q *Queue) Get(id string) (Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var j Job
	var runAfter, createdAt, updatedAt string
	err := q.db.QueryRow(
		`SELECT id, tenant, kind, payload, run_after, attempts, state, last_error, created_at, updated_at
		 FROM jobs WHERE id=?`, id,
	).Scan(&j.ID, &j.Tenant, &j.Kind, &j.Payload, &runAfter, &j.Attempts, &j.State, &j.LastError, &createdAt, &updatedAt)
	if err != nil {
		return Job{}, err
	}
	j.RunAfter, _ = parseTime(runAfter)
	j.CreatedAt, _ = parseTime(createdAt)
	j.UpdatedAt, _ = parseTime(updatedAt)
	return j, nil
}

// CountByState returns how many jobs are in the given state (optionally scoped
// to a tenant; pass "" for all tenants). Useful for tests and health checks.
func (q *Queue) CountByState(tenant, state string) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var n int
	var err error
	if tenant == "" {
		err = q.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE state=?`, state).Scan(&n)
	} else {
		err = q.db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE state=? AND tenant=?`, state, tenant).Scan(&n)
	}
	return n, err
}

func (q *Queue) log(msg string, fields map[string]any) {
	if q.logf != nil {
		q.logf(msg, fields)
	}
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, s)
}

func genID() string {
	buf := make([]byte, 12)
	rand.Read(buf)
	return hex.EncodeToString(buf)
}
