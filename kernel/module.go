package kernel

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel/config"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/resource"
)

// ModuleState represents the lifecycle state of a module.
type ModuleState int

const (
	ModuleStateRegistered ModuleState = iota
	ModuleStateInitialized
	ModuleStateRunning
	ModuleStateStopped
	ModuleStateFailed
)

func (s ModuleState) String() string {
	switch s {
	case ModuleStateRegistered:
		return "registered"
	case ModuleStateInitialized:
		return "initialized"
	case ModuleStateRunning:
		return "running"
	case ModuleStateStopped:
		return "stopped"
	case ModuleStateFailed:
		return "failed"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// HealthStatus represents the health of a module.
type HealthStatus struct {
	Healthy bool
	Message string
}

// Module is the interface that all Cyntr modules must implement.
type Module interface {
	Name() string
	Dependencies() []string
	Init(ctx context.Context, services *Services) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Health(ctx context.Context) HealthStatus
}

// Services provides access to kernel services during module initialization.
type Services struct {
	Bus       *ipc.Bus
	Config    *config.Store
	Resources *resource.Manager
}
