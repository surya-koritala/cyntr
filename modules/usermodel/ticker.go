package usermodel

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Ticker runs the distiller on a cron schedule. Lives alongside the
// distiller in this package rather than inside the scheduler module
// because the scheduler is hard-wired to agent.chat jobs — pushing the
// distill cadence through there would couple the two more than necessary.
type Ticker struct {
	distiller *Distiller
	schedule  cronExpr
	stop      chan struct{}
	stopped   chan struct{}
	logger    func(string, map[string]any)
	mu        sync.Mutex
	running   bool
}

// NewTicker builds a Ticker. cronExpr is a 5-field cron expression in UTC;
// the default is "0 4 * * *" (04:00 UTC daily). Pass a non-default via
// CYNTR_USERMODEL_DISTILL_CRON.
func NewTicker(distiller *Distiller, cronExprStr string, logger func(string, map[string]any)) (*Ticker, error) {
	if cronExprStr == "" {
		cronExprStr = "0 4 * * *"
	}
	sched, err := parseCronExpr(cronExprStr)
	if err != nil {
		return nil, err
	}
	return &Ticker{
		distiller: distiller,
		schedule:  sched,
		stop:      make(chan struct{}),
		stopped:   make(chan struct{}),
		logger:    logger,
	}, nil
}

// Start begins the background loop. Calling Start twice is a no-op.
func (t *Ticker) Start() {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return
	}
	t.running = true
	t.mu.Unlock()
	go t.loop()
}

// Stop signals the loop to exit and waits for it to drain.
func (t *Ticker) Stop() {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()
	close(t.stop)
	<-t.stopped
}

func (t *Ticker) loop() {
	defer close(t.stopped)
	// Tick at 1-minute granularity so we don't miss the matching minute by
	// more than 59s — cheaper than re-computing Next() and sleeping.
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	var lastRun time.Time
	for {
		select {
		case <-t.stop:
			return
		case now := <-ticker.C:
			now = now.UTC()
			if !t.schedule.matches(now) {
				continue
			}
			// Same-minute guard. ticker.C fires once per minute so this
			// almost never triggers, but defensive coding around clock
			// hiccups is cheap.
			if !lastRun.IsZero() && now.Sub(lastRun) < time.Minute {
				continue
			}
			lastRun = now
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			results := t.distiller.Tick(ctx)
			cancel()
			if t.logger != nil {
				skipped, success, errored := 0, 0, 0
				for _, r := range results {
					switch {
					case r.Error != "":
						errored++
					case r.Skipped:
						skipped++
					default:
						success++
					}
				}
				t.logger("usermodel distill tick complete", map[string]any{
					"total":   len(results),
					"success": success,
					"skipped": skipped,
					"errored": errored,
				})
			}
		}
	}
}

// ---- minimal in-package cron parser ----
//
// We carry a small parser rather than importing modules/scheduler to keep
// the dependency direction clean. The scheduler package depends on the
// agent package (for ChatRequest), which depends on usermodel — pulling
// the scheduler in here would create a cycle.

type cronExpr struct {
	mins, hours, days, months, weekdays []int
}

func (c cronExpr) matches(t time.Time) bool {
	return inList(c.mins, t.Minute()) &&
		inList(c.hours, t.Hour()) &&
		inList(c.days, t.Day()) &&
		inList(c.months, int(t.Month())) &&
		inList(c.weekdays, int(t.Weekday()))
}

func parseCronExpr(s string) (cronExpr, error) {
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return cronExpr{}, errBadCron
	}
	var c cronExpr
	var err error
	if c.mins, err = parseCronField(fields[0], 0, 59); err != nil {
		return cronExpr{}, err
	}
	if c.hours, err = parseCronField(fields[1], 0, 23); err != nil {
		return cronExpr{}, err
	}
	if c.days, err = parseCronField(fields[2], 1, 31); err != nil {
		return cronExpr{}, err
	}
	if c.months, err = parseCronField(fields[3], 1, 12); err != nil {
		return cronExpr{}, err
	}
	if c.weekdays, err = parseCronField(fields[4], 0, 6); err != nil {
		return cronExpr{}, err
	}
	return c, nil
}

func parseCronField(f string, lo, hi int) ([]int, error) {
	if f == "*" {
		out := make([]int, 0, hi-lo+1)
		for i := lo; i <= hi; i++ {
			out = append(out, i)
		}
		return out, nil
	}
	var out []int
	for _, part := range strings.Split(f, ",") {
		// Step: */N or LO-HI/N
		if i := strings.Index(part, "/"); i >= 0 {
			step, err := strconv.Atoi(part[i+1:])
			if err != nil || step <= 0 {
				return nil, errBadCron
			}
			start, end := lo, hi
			if part[:i] != "*" {
				r, err := parseCronRange(part[:i], lo, hi)
				if err != nil {
					return nil, err
				}
				start, end = r[0], r[len(r)-1]
			}
			for v := start; v <= end; v += step {
				out = append(out, v)
			}
			continue
		}
		if strings.Contains(part, "-") {
			r, err := parseCronRange(part, lo, hi)
			if err != nil {
				return nil, err
			}
			out = append(out, r...)
			continue
		}
		v, err := strconv.Atoi(part)
		if err != nil || v < lo || v > hi {
			return nil, errBadCron
		}
		out = append(out, v)
	}
	return out, nil
}

func parseCronRange(s string, lo, hi int) ([]int, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return nil, errBadCron
	}
	a, err1 := strconv.Atoi(parts[0])
	b, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || a < lo || b > hi || a > b {
		return nil, errBadCron
	}
	out := make([]int, 0, b-a+1)
	for v := a; v <= b; v++ {
		out = append(out, v)
	}
	return out, nil
}

func inList(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

type cronErr string

func (e cronErr) Error() string { return string(e) }

const errBadCron = cronErr("usermodel: invalid cron expression")
