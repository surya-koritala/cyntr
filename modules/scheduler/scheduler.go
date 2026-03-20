package scheduler

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

// Scheduler is a kernel module that runs scheduled agent tasks.
type Scheduler struct {
	mu      sync.RWMutex
	bus     *ipc.Bus
	jobs    map[string]*Job
	stop    chan struct{}
	stopped chan struct{}
}

func New() *Scheduler {
	return &Scheduler{
		jobs:    make(map[string]*Job),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

func (s *Scheduler) Name() string           { return "scheduler" }
func (s *Scheduler) Dependencies() []string { return []string{"agent_runtime"} }

func (s *Scheduler) Init(ctx context.Context, svc *kernel.Services) error {
	s.bus = svc.Bus
	return nil
}

func (s *Scheduler) Start(ctx context.Context) error {
	s.bus.Handle("scheduler", "scheduler.add", s.handleAdd)
	s.bus.Handle("scheduler", "scheduler.remove", s.handleRemove)
	s.bus.Handle("scheduler", "scheduler.list", s.handleList)

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
		if now.Before(job.NextRun) {
			continue
		}

		// Run the job
		go s.executeJob(job)

		job.LastRun = now
		job.NextRun = now.Add(job.Interval)
	}
}

func (s *Scheduler) executeJob(job *Job) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	s.bus.Request(ctx, ipc.Message{
		Source: "scheduler", Target: "agent_runtime", Topic: "agent.chat",
		Payload: agent.ChatRequest{
			Agent: job.Agent, Tenant: job.Tenant,
			User: "scheduler", Message: job.Message,
		},
	})
}

func (s *Scheduler) handleAdd(msg ipc.Message) (ipc.Message, error) {
	job, ok := msg.Payload.(Job)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected Job, got %T", msg.Payload)
	}

	s.mu.Lock()
	job.Enabled = true
	job.NextRun = time.Now().Add(job.Interval)
	s.jobs[job.ID] = &job
	s.mu.Unlock()

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
