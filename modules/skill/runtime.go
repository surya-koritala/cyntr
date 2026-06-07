package skill

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
)

// OpenClawLoader is a function that loads an OpenClaw SKILL.md file into an InstalledSkill.
// It is injected to avoid an import cycle between the skill and skill/compat packages.
type OpenClawLoader func(path string) (*InstalledSkill, error)

// Runtime is the Skill Runtime kernel module.
type Runtime struct {
	bus            *ipc.Bus
	registry       *Registry
	openClawLoader OpenClawLoader

	// candidates persists proposed (not-yet-approved) skills. When nil, the
	// propose/approve topics return a "not configured" error.
	candidates *CandidateStore
	// autoActivateSafe lets the policy layer opt in to auto-activating
	// proposed skills whose capabilities are safe (no shell/network/fs).
	// Skills touching shell/network/fs always require operator approval.
	autoActivateSafe bool

	// skillBaseDir confines filesystem paths accepted by skill.install and
	// skill.import_openclaw. When set, a supplied path must resolve to a
	// location inside this directory. When empty (not configured), paths are
	// still rejected if they contain any ".." traversal component, so a caller
	// cannot escape whatever directory it intends to load from.
	skillBaseDir string
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

// SetSkillBaseDir confines skill.install and skill.import_openclaw to dir.
// Supplied paths must resolve inside dir. Call before Start. When unset, the
// loaders still reject any path containing a ".." traversal component.
func (r *Runtime) SetSkillBaseDir(dir string) {
	r.skillBaseDir = dir
}

// confinePath validates a caller-supplied filesystem path for the skill
// loaders. It rejects ".." traversal and, when a base dir is configured,
// requires the path to resolve inside it.
func (r *Runtime) confinePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	clean := filepath.Clean(p)
	// Reject any explicit parent traversal regardless of base configuration.
	for _, seg := range strings.Split(filepath.ToSlash(clean), "/") {
		if seg == ".." {
			return "", fmt.Errorf("path traversal (\"..\") is not allowed: %q", p)
		}
	}
	if r.skillBaseDir == "" {
		return clean, nil
	}
	absBase, err := filepath.Abs(r.skillBaseDir)
	if err != nil {
		return "", err
	}
	candidate := clean
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(absBase, candidate)
	}
	absPath, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if absPath != absBase && !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes skill base directory", p)
	}
	return absPath, nil
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
	dir, err := r.confinePath(dir)
	if err != nil {
		return ipc.Message{}, fmt.Errorf("skill.install: %w", err)
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
	path, err := r.confinePath(path)
	if err != nil {
		return ipc.Message{}, fmt.Errorf("skill.import_openclaw: %w", err)
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
		// Auto-activation must never silently overwrite a skill that already
		// exists (catalog/trusted/operator-approved). Refuse to clobber it;
		// the candidate stays pending for explicit operator approval.
		if existing, exists := r.registry.Get(cand.Name); exists && isTrustedSkill(existing) {
			r.candidates.SetStatus(id, CandidatePending, "awaiting operator approval: name collides with an existing trusted skill")
		} else if err := r.activate(cand, false); err == nil {
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
	var status, tenant string
	switch p := msg.Payload.(type) {
	case CandidatesQuery:
		status, tenant = p.Status, p.Tenant
	case string:
		status = p // legacy bare status (operator/system view)
	}
	list, err := r.candidates.ListForTenant(status, tenant)
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
	var id int64
	var callerTenant string
	switch p := msg.Payload.(type) {
	case ApproveRequest:
		id, callerTenant = p.ID, p.Tenant
	default:
		v, ok := toID(msg.Payload)
		if !ok {
			return ipc.Message{}, fmt.Errorf("skill.candidate_approve: expected candidate id, got %T", msg.Payload)
		}
		id = v
	}
	cand, err := r.candidates.Get(id)
	if err != nil {
		return ipc.Message{}, err
	}
	if err := scopeToTenant("skill.candidate_approve", callerTenant, cand); err != nil {
		return ipc.Message{}, err
	}
	if cand.Status != CandidatePending {
		return ipc.Message{}, fmt.Errorf("skill.candidate_approve: candidate %d is %q, not pending", id, cand.Status)
	}
	// Operator override: overwriting a trusted skill is permitted here because
	// a human (or tenant-scoped principal) is explicitly approving.
	if err := r.activate(cand, true); err != nil {
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
	cand, err := r.candidates.Get(req.ID)
	if err != nil {
		return ipc.Message{}, err
	}
	if err := scopeToTenant("skill.candidate_reject", req.Tenant, cand); err != nil {
		return ipc.Message{}, err
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
func (r *Runtime) activate(c Candidate, allowOverwrite bool) error {
	s := c.toInstalledSkill()
	if existing, exists := r.registry.Get(s.Manifest.Name); exists {
		if !allowOverwrite && isTrustedSkill(existing) {
			return fmt.Errorf("skill %q already exists as a trusted skill; refusing to overwrite without operator approval", s.Manifest.Name)
		}
		return r.registry.ReplaceWithBackup(s)
	}
	return r.registry.InstallDirect(s)
}

// isTrustedSkill reports whether an installed skill is a curated/catalog or
// otherwise non-candidate skill that an auto-activated candidate must not
// silently overwrite. Candidate-minted skills carry an "agent:" author and the
// candidate version; anything else (catalog skills, hand-written skills,
// operator-approved skills) is treated as trusted.
func isTrustedSkill(s *InstalledSkill) bool {
	if s == nil {
		return false
	}
	if s.Path == "embedded://catalog" {
		return true
	}
	if s.Manifest.Version == CandidateVersion && strings.HasPrefix(s.Manifest.Author, "agent:") {
		return false
	}
	return true
}

// handleRollback reverts a skill to its most recent prior version.
func (r *Runtime) handleRollback(msg ipc.Message) (ipc.Message, error) {
	var name, callerTenant string
	switch p := msg.Payload.(type) {
	case RollbackRequest:
		name, callerTenant = p.Name, p.Tenant
	case string:
		name = p // legacy bare name (operator/system view)
	default:
		return ipc.Message{}, fmt.Errorf("skill.rollback: expected skill name, got %T", msg.Payload)
	}
	// Tenant scoping: a principal-scoped caller may only roll back a skill its
	// own tenant originated. An empty callerTenant denotes a trusted operator.
	if callerTenant != "" {
		if r.candidates == nil {
			return ipc.Message{}, fmt.Errorf("skill.rollback: candidate store not configured")
		}
		owns, err := r.candidates.TenantOwnsSkill(callerTenant, name)
		if err != nil {
			return ipc.Message{}, err
		}
		if !owns {
			return ipc.Message{}, fmt.Errorf("skill.rollback: skill %q does not belong to caller tenant", name)
		}
	}
	if err := r.registry.Rollback(name); err != nil {
		return ipc.Message{}, err
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

// scopeToTenant enforces tenant isolation on a candidate-targeting handler.
// When the caller presents a tenant (the authenticated principal's tenant), the
// targeted candidate must belong to that tenant or the request is refused, so a
// tenant cannot approve/reject another tenant's candidate. An empty callerTenant
// denotes a trusted operator/system caller (legacy bare-id/name payloads) and is
// allowed through; principal-aware callers should always pass their tenant.
func scopeToTenant(op, callerTenant string, cand Candidate) error {
	if callerTenant == "" {
		return nil
	}
	if cand.Tenant != callerTenant {
		return fmt.Errorf("%s: candidate %d belongs to another tenant", op, cand.ID)
	}
	return nil
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
