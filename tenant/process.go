package tenant

import (
	"fmt"
	"os/exec"
	"sync"
)

// ProcessState represents the state of a supervised process.
type ProcessState int

const (
	ProcessRunning ProcessState = iota
	ProcessStopped
	ProcessFailed
)

func (s ProcessState) String() string {
	switch s {
	case ProcessRunning:
		return "running"
	case ProcessStopped:
		return "stopped"
	case ProcessFailed:
		return "failed"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// SupervisedProcess represents a process-isolated agent.
type SupervisedProcess struct {
	ID     string
	Tenant string
	Cmd    *exec.Cmd
	State  ProcessState
	PID    int
}

// ProcessSupervisor manages process-isolated tenant workloads.
type ProcessSupervisor struct {
	mu        sync.RWMutex
	processes map[string]*SupervisedProcess // id -> process
}

// NewProcessSupervisor creates a process supervisor.
func NewProcessSupervisor() *ProcessSupervisor {
	return &ProcessSupervisor{
		processes: make(map[string]*SupervisedProcess),
	}
}

// Spawn starts a new process for a tenant workload.
// The command is executed with the given args.
func (ps *ProcessSupervisor) Spawn(id, tenant, command string, args ...string) (*SupervisedProcess, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if _, exists := ps.processes[id]; exists {
		return nil, fmt.Errorf("process %q already exists", id)
	}

	cmd := exec.Command(command, args...)

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn process: %w", err)
	}

	proc := &SupervisedProcess{
		ID:     id,
		Tenant: tenant,
		Cmd:    cmd,
		State:  ProcessRunning,
		PID:    cmd.Process.Pid,
	}

	ps.processes[id] = proc

	// Monitor process in background
	go func() {
		cmd.Wait()
		ps.mu.Lock()
		if p, ok := ps.processes[id]; ok {
			p.State = ProcessStopped
		}
		ps.mu.Unlock()
	}()

	return proc, nil
}

// Stop terminates a process gracefully.
func (ps *ProcessSupervisor) Stop(id string) error {
	ps.mu.Lock()
	proc, ok := ps.processes[id]
	ps.mu.Unlock()

	if !ok {
		return fmt.Errorf("process %q not found", id)
	}

	if proc.Cmd.Process == nil {
		return nil
	}

	// Try graceful kill first
	proc.Cmd.Process.Kill()

	ps.mu.Lock()
	proc.State = ProcessStopped
	ps.mu.Unlock()

	return nil
}

// Get returns a process by ID.
func (ps *ProcessSupervisor) Get(id string) (*SupervisedProcess, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.processes[id]
	return p, ok
}

// List returns all processes for a tenant.
func (ps *ProcessSupervisor) List(tenant string) []*SupervisedProcess {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	var result []*SupervisedProcess
	for _, p := range ps.processes {
		if p.Tenant == tenant {
			result = append(result, p)
		}
	}
	return result
}

// ListAll returns all supervised processes.
func (ps *ProcessSupervisor) ListAll() []*SupervisedProcess {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	result := make([]*SupervisedProcess, 0, len(ps.processes))
	for _, p := range ps.processes {
		result = append(result, p)
	}
	return result
}

// StopAll terminates all processes.
func (ps *ProcessSupervisor) StopAll() {
	ps.mu.RLock()
	ids := make([]string, 0)
	for id := range ps.processes {
		ids = append(ids, id)
	}
	ps.mu.RUnlock()

	for _, id := range ids {
		ps.Stop(id)
	}
}

// HealthCheck returns the state of a process.
func (ps *ProcessSupervisor) HealthCheck(id string) (ProcessState, error) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.processes[id]
	if !ok {
		return ProcessFailed, fmt.Errorf("process %q not found", id)
	}
	return p.State, nil
}
