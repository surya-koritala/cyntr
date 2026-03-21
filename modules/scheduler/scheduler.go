package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/channel"
)

var logger = log.Default().WithModule("scheduler")

// Scheduler is a kernel module that runs scheduled agent tasks.
type Scheduler struct {
	mu        sync.RWMutex
	bus       *ipc.Bus
	jobs      map[string]*Job
	history   map[string][]JobRun
	storePath string
	stop      chan struct{}
	stopped   chan struct{}
}

func New(storePath string) *Scheduler {
	return &Scheduler{
		jobs:      make(map[string]*Job),
		history:   make(map[string][]JobRun),
		storePath: storePath,
		stop:      make(chan struct{}),
		stopped:   make(chan struct{}),
	}
}

func (s *Scheduler) Name() string           { return "scheduler" }
func (s *Scheduler) Dependencies() []string { return []string{"agent_runtime"} }

func (s *Scheduler) Init(ctx context.Context, svc *kernel.Services) error {
	s.bus = svc.Bus
	return nil
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.loadJobs()

	s.bus.Handle("scheduler", "scheduler.add", s.handleAdd)
	s.bus.Handle("scheduler", "scheduler.remove", s.handleRemove)
	s.bus.Handle("scheduler", "scheduler.list", s.handleList)
	s.bus.Handle("scheduler", "scheduler.history", s.handleHistory)

	go s.runLoop()
	return nil
}

func (s *Scheduler) Stop(ctx context.Context) error {
	close(s.stop)
	<-s.stopped
	return nil
}

func (s *Scheduler) Health(ctx context.Context) kernel.HealthStatus {
	s.mu.RLock()
	count := len(s.jobs)
	s.mu.RUnlock()
	return kernel.HealthStatus{Healthy: true, Message: fmt.Sprintf("%d scheduled jobs", count)}
}

func (s *Scheduler) runLoop() {
	defer close(s.stopped)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case now := <-ticker.C:
			s.checkAndRun(now)
		}
	}
}

func (s *Scheduler) checkAndRun(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, job := range s.jobs {
		if !job.Enabled {
			continue
		}

		shouldRun := false

		if job.CronExpr != "" {
			cs, err := ParseCron(job.CronExpr)
			if err == nil && cs.Matches(now) && now.After(job.LastRun.Add(time.Minute)) {
				shouldRun = true
			}
		} else if !now.Before(job.NextRun) {
			shouldRun = true
		}

		if !shouldRun {
			continue
		}

		// Check dependencies before running
		if !s.checkDependencies(job) {
			continue
		}

		// Run the job
		go s.executeJob(job)

		job.LastRun = now
		if job.CronExpr != "" {
			cs, err := ParseCron(job.CronExpr)
			if err == nil {
				job.NextRun = cs.Next(now)
			}
		} else {
			job.NextRun = now.Add(job.Interval)
		}
	}
}

func (s *Scheduler) executeJob(job *Job) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "scheduler", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent: job.Agent, Tenant: job.Tenant,
			User: "scheduler", Message: job.Message,
		},
	})
	if err != nil {
		logger.Error("scheduled job failed", map[string]any{"job_id": job.ID, "agent": job.Agent, "error": err.Error()})
		// Record failure
		s.recordJobRun(job.ID, JobRun{
			ID: fmt.Sprintf("jr_%d", time.Now().UnixNano()),
			JobID: job.ID, Status: "failure",
			Error: err.Error(), StartedAt: start, Duration: time.Since(start),
		})
		return
	}

	chatResp, ok := resp.Payload.(agent.ChatResponse)
	if !ok {
		logger.Warn("scheduled job unexpected response", map[string]any{"job_id": job.ID})
		s.recordJobRun(job.ID, JobRun{
			ID: fmt.Sprintf("jr_%d", time.Now().UnixNano()),
			JobID: job.ID, Status: "failure",
			Error: "unexpected response type", StartedAt: start, Duration: time.Since(start),
		})
		return
	}

	// Check condition
	if job.Condition != nil {
		switch job.Condition.Type {
		case "output_changed":
			s.mu.RLock()
			prevRuns := s.history[job.ID]
			s.mu.RUnlock()
			if len(prevRuns) > 0 && prevRuns[len(prevRuns)-1].Output == chatResp.Content {
				logger.Info("job condition not met: output unchanged", map[string]any{"job_id": job.ID})
				return
			}
		case "output_matches":
			if job.Condition.Pattern != "" {
				matched, _ := regexp.MatchString(job.Condition.Pattern, chatResp.Content)
				if !matched {
					logger.Info("job condition not met: pattern not matched", map[string]any{"job_id": job.ID})
					return
				}
			}
		}
	}

	// Record success
	s.recordJobRun(job.ID, JobRun{
		ID: fmt.Sprintf("jr_%d", time.Now().UnixNano()),
		JobID: job.ID, Status: "success",
		Output: chatResp.Content, StartedAt: start, Duration: time.Since(start),
	})

	// Deliver results to configured channel
	if job.DestChannel != "" && job.DestChannelID != "" {
		_, err := s.bus.Request(ctx, ipc.Message{
			Source: "scheduler", Target: "channel", Topic: "channel.send",
			Payload: channel.OutboundMessage{
				Channel:   job.DestChannel,
				ChannelID: job.DestChannelID,
				Text:      chatResp.Content,
			},
		})
		if err != nil {
			logger.Warn("job delivery failed", map[string]any{"job_id": job.ID, "channel": job.DestChannel, "error": err.Error()})
		}
	}
}

func (s *Scheduler) checkDependencies(job *Job) bool {
	if len(job.DependsOn) == 0 {
		return true
	}
	for _, depID := range job.DependsOn {
		dep, ok := s.jobs[depID]
		if !ok || !dep.Enabled {
			return false
		}
		// Check if dependency ran this cycle (LastRun is recent enough)
		if dep.LastRun.IsZero() || dep.LastRun.Before(job.LastRun) {
			return false
		}
	}
	return true
}

func (s *Scheduler) recordJobRun(jobID string, run JobRun) {
	s.mu.Lock()
	runs := s.history[jobID]
	runs = append(runs, run)
	if len(runs) > 20 {
		runs = runs[len(runs)-20:]
	}
	s.history[jobID] = runs
	s.mu.Unlock()
}

func (s *Scheduler) handleAdd(msg ipc.Message) (ipc.Message, error) {
	job, ok := msg.Payload.(Job)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected Job, got %T", msg.Payload)
	}

	// Validate cron expression if provided
	if job.CronExpr != "" {
		cs, err := ParseCron(job.CronExpr)
		if err != nil {
			return ipc.Message{}, fmt.Errorf("invalid cron expression: %w", err)
		}
		job.NextRun = cs.Next(time.Now())
	} else {
		job.NextRun = time.Now().Add(job.Interval)
	}

	s.mu.Lock()
	job.Enabled = true
	s.jobs[job.ID] = &job
	s.mu.Unlock()

	s.saveJobs()

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (s *Scheduler) handleRemove(msg ipc.Message) (ipc.Message, error) {
	id, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string, got %T", msg.Payload)
	}

	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()

	s.saveJobs()

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (s *Scheduler) handleList(msg ipc.Message) (ipc.Message, error) {
	s.mu.RLock()
	jobs := make([]Job, 0, len(s.jobs))
	for _, j := range s.jobs {
		jobs = append(jobs, *j)
	}
	s.mu.RUnlock()

	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: jobs}, nil
}

func (s *Scheduler) handleHistory(msg ipc.Message) (ipc.Message, error) {
	jobID, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string")
	}
	s.mu.RLock()
	runs := s.history[jobID]
	s.mu.RUnlock()
	if runs == nil {
		runs = []JobRun{}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: runs}, nil
}

func (s *Scheduler) saveJobs() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	jobs := make([]*Job, 0)
	for _, j := range s.jobs {
		jobs = append(jobs, j)
	}
	data, _ := json.Marshal(jobs)
	os.WriteFile(s.storePath, data, 0644)
}

func (s *Scheduler) loadJobs() {
	data, err := os.ReadFile(s.storePath)
	if err != nil {
		return
	}
	var jobs []*Job
	if json.Unmarshal(data, &jobs) == nil {
		for _, j := range jobs {
			s.jobs[j.ID] = j
		}
	}
}
