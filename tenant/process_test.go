package tenant

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestProcessSupervisorSpawn(t *testing.T) {
	ps := NewProcessSupervisor()
	proc, err := ps.Spawn("test-1", "finance", "sleep", "10")
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer ps.Stop("test-1")

	if proc.PID <= 0 {
		t.Fatal("expected valid PID")
	}
	if proc.State != ProcessRunning {
		t.Fatalf("expected running, got %s", proc.State)
	}
	if proc.Tenant != "finance" {
		t.Fatalf("expected finance, got %q", proc.Tenant)
	}
}

func TestProcessSupervisorSpawnDuplicate(t *testing.T) {
	ps := NewProcessSupervisor()
	ps.Spawn("test-1", "finance", "sleep", "10")
	defer ps.StopAll()

	_, err := ps.Spawn("test-1", "finance", "sleep", "10")
	if err == nil {
		t.Fatal("expected error for duplicate")
	}
}

func TestProcessSupervisorStop(t *testing.T) {
	ps := NewProcessSupervisor()
	ps.Spawn("test-1", "finance", "sleep", "10")

	if err := ps.Stop("test-1"); err != nil {
		t.Fatalf("stop: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	proc, _ := ps.Get("test-1")
	if proc.State != ProcessStopped {
		t.Fatalf("expected stopped, got %s", proc.State)
	}
}

func TestProcessSupervisorStopNotFound(t *testing.T) {
	ps := NewProcessSupervisor()
	if err := ps.Stop("nonexistent"); err == nil {
		t.Fatal("expected error")
	}
}

func TestProcessSupervisorGet(t *testing.T) {
	ps := NewProcessSupervisor()
	ps.Spawn("test-1", "finance", "sleep", "10")
	defer ps.StopAll()

	proc, ok := ps.Get("test-1")
	if !ok {
		t.Fatal("expected found")
	}
	if proc.ID != "test-1" {
		t.Fatalf("got %q", proc.ID)
	}
}

func TestProcessSupervisorList(t *testing.T) {
	ps := NewProcessSupervisor()
	ps.Spawn("f-1", "finance", "sleep", "10")
	ps.Spawn("f-2", "finance", "sleep", "10")
	ps.Spawn("m-1", "marketing", "sleep", "10")
	defer ps.StopAll()

	finProcs := ps.List("finance")
	if len(finProcs) != 2 {
		t.Fatalf("expected 2 finance procs, got %d", len(finProcs))
	}

	all := ps.ListAll()
	if len(all) != 3 {
		t.Fatalf("expected 3 total, got %d", len(all))
	}
}

func TestProcessSupervisorHealthCheck(t *testing.T) {
	ps := NewProcessSupervisor()
	ps.Spawn("test-1", "finance", "sleep", "10")
	defer ps.StopAll()

	state, err := ps.HealthCheck("test-1")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if state != ProcessRunning {
		t.Fatalf("expected running, got %s", state)
	}
}

func TestProcessSupervisorProcessExits(t *testing.T) {
	ps := NewProcessSupervisor()
	ps.Spawn("short", "finance", "echo", "done")

	time.Sleep(200 * time.Millisecond)

	proc, _ := ps.Get("short")
	if proc.State != ProcessStopped {
		t.Fatalf("expected stopped after exit, got %s", proc.State)
	}
}

func TestProcessStateString(t *testing.T) {
	tests := []struct {
		s    ProcessState
		want string
	}{
		{ProcessRunning, "running"},
		{ProcessStopped, "stopped"},
		{ProcessFailed, "failed"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}

func TestProcessSupervisorConcurrentSpawn(t *testing.T) {
	ps := NewProcessSupervisor()
	defer ps.StopAll()

	var wg sync.WaitGroup
	errors := make([]error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := ps.Spawn(fmt.Sprintf("proc-%d", idx), "finance", "sleep", "10")
			errors[idx] = err
		}(i)
	}

	wg.Wait()

	// All should succeed (unique IDs)
	for i, err := range errors {
		if err != nil {
			t.Fatalf("spawn %d: %v", i, err)
		}
	}

	all := ps.ListAll()
	if len(all) != 20 {
		t.Fatalf("expected 20, got %d", len(all))
	}
}

func TestProcessSupervisorCommandNotFound(t *testing.T) {
	ps := NewProcessSupervisor()
	_, err := ps.Spawn("bad", "t", "/nonexistent/command/xyz")
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
}

func TestProcessSupervisorStopAllConcurrent(t *testing.T) {
	ps := NewProcessSupervisor()
	for i := 0; i < 5; i++ {
		ps.Spawn(fmt.Sprintf("p-%d", i), "t", "sleep", "60")
	}

	// StopAll should not panic
	ps.StopAll()

	time.Sleep(200 * time.Millisecond)

	// All should be stopped
	for i := 0; i < 5; i++ {
		proc, ok := ps.Get(fmt.Sprintf("p-%d", i))
		if ok && proc.State == ProcessRunning {
			t.Fatalf("process p-%d still running", i)
		}
	}
}
