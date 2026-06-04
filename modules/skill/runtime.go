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

	// candidates persists proposed (not-yet-approved) skills. When nil, the
	// propose/approve topics return a "not configured" error.
	candidates *CandidateStore
	// autoActivateSafe lets the policy layer opt in to auto-activating
	// proposed skills whose capabilities are safe (no shell/network/fs).
	// Skills touching shell/network/fs always require operator approval.
	autoActivateSafe bool
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

// SetCandidateStore wires the persistent candidate store. Call before Start.
func (r *Runtime) SetCandidateStore(cs *CandidateStore) {
	r.candidates = cs
}

// SetAutoActivateSafe sets the policy decision for auto-activating proposed
// skills with safe capabilities. Off by default.
func (r *Runtime) SetAutoActivateSafe(v bool) {
	r.autoActivateSafe = v
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
	r.bus.Handle("skill_runtime", TopicPropose, r.handlePropose)
	r.bus.Handle("skill_runtime", TopicCandidates, r.handleCandidates)
	r.bus.Handle("skill_runtime", TopicCandidateApprove, r.handleCandidateApprove)
	r.bus.Handle("skill_runtime", TopicCandidateReject, r.handleCandidateReject)
	r.bus.Handle("skill_runtime", TopicRollback, r.handleRollback)

	// Load embedded catalog skills
	for _, catalogSkill := range LoadEmbeddedCatalog() {
		if catalogSkill.Manifest.Name == "_catalog-readme" || catalogSkill.Manifest.Name == "openclaw-_catalog-readme" {
			continue // skip placeholder
		}
		if err := r.registry.InstallDirect(catalogSkill); err != nil {
			// Already installed or name conflict — skip silently
			continue
		}
	}

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

// handlePropose persists a proposed skill as a pending candidate. It is never
// auto-activated unless its capabilities are safe (no shell/network/fs) AND
// policy has opted in via SetAutoActivateSafe — otherwise it waits for an
// operator to approve it.
func (r *Runtime) handlePropose(msg ipc.Message) (ipc.Message, error) {
	if r.candidates == nil {
		return ipc.Message{}, fmt.Errorf("skill.propose: candidate store not configured")
	}
	req, ok := msg.Payload.(ProposeRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("skill.propose: expected ProposeRequest, got %T", msg.Payload)
	}
	if err := validateProposal(req); err != nil {
		return ipc.Message{}, err
	}
	id, err := r.candidates.Propose(Candidate{
		Tenant: req.Tenant, Name: req.Name, Description: req.Description,
		Instructions: req.Instructions, Capabilities: req.Capabilities, SourceAgent: req.SourceAgent,
	})
	if err != nil {
		return ipc.Message{}, err
	}
	result := ProposeResult{ID: id, Status: CandidatePending}
	if r.autoActivateSafe && req.Capabilities.IsSafe() {
		cand, _ := r.candidates.Get(id)
		if err := r.activate(cand); err == nil {
			r.candidates.SetStatus(id, CandidateApproved, "auto-activated (safe capabilities)")
			result.Status, result.Activated = CandidateApproved, true
		}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: result}, nil
}

// handleCandidates lists candidates by status (default pending).
func (r *Runtime) handleCandidates(msg ipc.Message) (ipc.Message, error) {
	if r.candidates == nil {
		return ipc.Message{}, fmt.Errorf("skill.candidates: candidate store not configured")
	}
	status, _ := msg.Payload.(string)
	list, err := r.candidates.List(status)
	if err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: list}, nil
}

// handleCandidateApprove installs a pending candidate into the registry. This
// is the operator override — it works regardless of capabilities.
func (r *Runtime) handleCandidateApprove(msg ipc.Message) (ipc.Message, error) {
	if r.candidates == nil {
		return ipc.Message{}, fmt.Errorf("skill.candidate_approve: candidate store not configured")
	}
	id, ok := toID(msg.Payload)
	if !ok {
		return ipc.Message{}, fmt.Errorf("skill.candidate_approve: expected candidate id, got %T", msg.Payload)
	}
	cand, err := r.candidates.Get(id)
	if err != nil {
		return ipc.Message{}, err
	}
	if cand.Status != CandidatePending {
		return ipc.Message{}, fmt.Errorf("skill.candidate_approve: candidate %d is %q, not pending", id, cand.Status)
	}
	if err := r.activate(cand); err != nil {
		return ipc.Message{}, err
	}
	if err := r.candidates.SetStatus(id, CandidateApproved, "approved by operator"); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

// handleCandidateReject marks a candidate rejected without installing it.
func (r *Runtime) handleCandidateReject(msg ipc.Message) (ipc.Message, error) {
	if r.candidates == nil {
		return ipc.Message{}, fmt.Errorf("skill.candidate_reject: candidate store not configured")
	}
	req, ok := msg.Payload.(RejectRequest)
	if !ok {
		return ipc.Message{}, fmt.Errorf("skill.candidate_reject: expected RejectRequest, got %T", msg.Payload)
	}
	if err := r.candidates.SetStatus(req.ID, CandidateRejected, req.Reason); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

// activate installs an approved candidate into the registry, where it is
// indistinguishable from a hand-written skill and loadable by skill_router.
// When a skill of that name already exists, the candidate is treated as an
// improved version: it replaces the current one but the prior version is kept
// for rollback (A3).
func (r *Runtime) activate(c Candidate) error {
	s := c.toInstalledSkill()
	if _, exists := r.registry.Get(s.Manifest.Name); exists {
		return r.registry.ReplaceWithBackup(s)
	}
	return r.registry.InstallDirect(s)
}

// handleRollback reverts a skill to its most recent prior version.
func (r *Runtime) handleRollback(msg ipc.Message) (ipc.Message, error) {
	name, ok := msg.Payload.(string)
	if !ok {
		return ipc.Message{}, fmt.Errorf("skill.rollback: expected skill name, got %T", msg.Payload)
	}
	if err := r.registry.Rollback(name); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

// toID coerces an IPC id payload (int64 in-process, float64 across JSON).
func toID(p any) (int64, bool) {
	switch v := p.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}
