package skill

import (
	"context"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// OpenClawLoader is a function that loads an OpenClaw SKILL.md file into an InstalledSkill.
// It is injected to avoid an import cycle between the skill and skill/compat packages.
type OpenClawLoader func(path string) (*InstalledSkill, error)

// Runtime is the Skill Runtime kernel module.
type Runtime struct {
	bus             *ipc.Bus
	registry        *Registry
	openClawLoader  OpenClawLoader
}

// NewRuntime creates a new Skill Runtime module.
func NewRuntime() *Runtime {
	return &Runtime{
		registry: NewRegistry(),
	}
}

// SetOpenClawLoader registers the loader used for skill.import_openclaw.
// Call this before Start (e.g. from main.go after wiring compat).
func (r *Runtime) SetOpenClawLoader(fn OpenClawLoader) {
	r.openClawLoader = fn
}

func (r *Runtime) Name() string           { return "skill_runtime" }
func (r *Runtime) Dependencies() []string { return nil }

func (r *Runtime) Init(ctx context.Context, svc *kernel.Services) error {
	r.bus = svc.Bus
	return nil
}

func (r *Runtime) Start(ctx context.Context) error {
	r.bus.Handle("skill_runtime", "skill.install", r.handleInstall)
	r.bus.Handle("skill_runtime", "skill.uninstall", r.handleUninstall)
	r.bus.Handle("skill_runtime", "skill.list", r.handleList)
	r.bus.Handle("skill_runtime", "skill.get", r.handleGet)
	r.bus.Handle("skill_runtime", "skill.instructions", r.handleInstructions)
	r.bus.Handle("skill_runtime", "skill.import_openclaw", r.handleImportOpenClaw)
	return nil
}

func (r *Runtime) Stop(ctx context.Context) error { return nil }

func (r *Runtime) Health(ctx context.Context) kernel.HealthStatus {
	return kernel.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("%d skills installed", len(r.registry.List())),
	}
}

func (r *Runtime) handleInstall(msg ipc.Message) (ipc.Message, error) {
	dir, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string path, got %T", msg.Payload)
	}
	if err := r.registry.Install(dir); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (r *Runtime) handleUninstall(msg ipc.Message) (ipc.Message, error) {
	name, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string name, got %T", msg.Payload)
	}
	if err := r.registry.Uninstall(name); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (r *Runtime) handleList(msg ipc.Message) (ipc.Message, error) {
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: r.registry.List()}, nil
}

func (r *Runtime) handleGet(msg ipc.Message) (ipc.Message, error) {
	name, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string name, got %T", msg.Payload)
	}
	skill, ok := r.registry.Get(name)
	if !ok {
		return ipc.Message{}, fmt.Errorf("skill %q not found", name)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: skill}, nil
}

func (r *Runtime) handleInstructions(msg ipc.Message) (ipc.Message, error) {
	names, ok := msg.Payload.([]string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected []string, got %T", msg.Payload)
	}
	return ipc.Message{
		Type:    ipc.MessageTypeResponse,
		Payload: r.registry.GetInstructions(names),
	}, nil
}

func (r *Runtime) handleImportOpenClaw(msg ipc.Message) (ipc.Message, error) {
	if r.openClawLoader == nil {
		return ipc.Message{}, fmt.Errorf("OpenClaw loader not configured")
	}
	path, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected string path, got %T", msg.Payload)
	}
	imported, err := r.openClawLoader(path)
	if err != nil {
		return ipc.Message{}, err
	}
	if err := r.registry.InstallDirect(imported); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: imported.Manifest.Name}, nil
}
