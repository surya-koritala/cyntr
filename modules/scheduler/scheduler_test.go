package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

func TestSchedulerImplementsModule(t *testing.T) {
	var _ kernel.Module = (*Scheduler)(nil)
}

func TestSchedulerAddAndListJobs(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	// Mock agent runtime
	bus.Handle("agent_runtime", "agent.chat", func(msg ipc.Message) (ipc.Message, error) {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: agent.ChatResponse{Content: "done"}}, nil
	})

	s := New()
	ctx := context.Background()
	s.Init(ctx, &kernel.Services{Bus: bus})
	s.Start(ctx)
	defer s.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "scheduler", Topic: "scheduler.add",
		Payload: Job{ID: "job1", Name: "daily-report", Tenant: "finance", Agent: "bot", Message: "Generate report", Interval: 24 * time.Hour},
	})

	resp, _ := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "scheduler", Topic: "scheduler.list",
	})
	jobs := resp.Payload.([]Job)
	if len(jobs) != 1 {
		t.Fatalf("expected 1, got %d", len(jobs))
	}
	if jobs[0].Name != "daily-report" {
		t.Fatalf("got %q", jobs[0].Name)
	}
}

func TestSchedulerRemoveJob(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	s := New()
	ctx := context.Background()
	s.Init(ctx, &kernel.Services{Bus: bus})
	s.Start(ctx)
	defer s.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "scheduler", Topic: "scheduler.add",
		Payload: Job{ID: "j1", Name: "test", Interval: time.Hour},
	})
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "scheduler", Topic: "scheduler.remove",
		Payload: "j1",
	})

	resp, _ := bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "scheduler", Topic: "scheduler.list",
	})
	jobs := resp.Payload.([]Job)
	if len(jobs) != 0 {
		t.Fatalf("expected 0, got %d", len(jobs))
	}
}

func TestSchedulerExecutesJob(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	executed := make(chan string, 1)
	bus.Handle("agent_runtime", "agent.chat", func(msg ipc.Message) (ipc.Message, error) {
		req := msg.Payload.(agent.ChatRequest)
		executed <- req.Message
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: agent.ChatResponse{Content: "ok"}}, nil
	})

	s := New()
	ctx := context.Background()
	s.Init(ctx, &kernel.Services{Bus: bus})
	s.Start(ctx)
	defer s.Stop(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Add job with 1-second interval
	bus.Request(reqCtx, ipc.Message{
		Source: "cli", Target: "scheduler", Topic: "scheduler.add",
		Payload: Job{ID: "fast", Name: "fast-job", Tenant: "t", Agent: "bot", Message: "run task", Interval: 1 * time.Second},
	})

	select {
	case msg := <-executed:
		if msg != "run task" {
			t.Fatalf("expected 'run task', got %q", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("job not executed within 3 seconds")
	}
}

func TestSchedulerHealthy(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	s := New()
	ctx := context.Background()
	s.Init(ctx, &kernel.Services{Bus: bus})
	s.Start(ctx)
	defer s.Stop(ctx)
	h := s.Health(ctx)
	if !h.Healthy {
		t.Fatalf("expected healthy: %s", h.Message)
	}
}

func TestSchedulerStopClean(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	s := New()
	ctx := context.Background()
	s.Init(ctx, &kernel.Services{Bus: bus})
	s.Start(ctx)
	// Stop should not hang
	err := s.Stop(ctx)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
}
