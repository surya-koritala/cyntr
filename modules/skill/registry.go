package skill

import (
	"fmt"
	"sort"
	"sync"
)

// Registry manages installed skills.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*InstalledSkill
	// versions keeps prior versions of a skill (most recent last) so an
	// approved improvement (A3) can be rolled back.
	versions map[string][]*InstalledSkill
}

// maxVersionHistory bounds how many prior versions of a skill we keep for
// rollback.
const maxVersionHistory = 10

// NewRegistry creates an empty skill registry.
func NewRegistry() *Registry {
	return &Registry{
		skills:   make(map[string]*InstalledSkill),
		versions: make(map[string][]*InstalledSkill),
	}
}

// ReplaceWithBackup installs s, pushing the current version (if any) onto the
// rollback stack so an improvement can be reverted. When no skill of that name
// exists it behaves like a fresh install.
func (r *Registry) ReplaceWithBackup(s *InstalledSkill) error {
	if s == nil || s.Manifest.Name == "" {
		return fmt.Errorf("skill: ReplaceWithBackup requires a named skill")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if prev, ok := r.skills[s.Manifest.Name]; ok {
		hist := append(r.versions[s.Manifest.Name], prev)
		if len(hist) > maxVersionHistory {
			hist = hist[len(hist)-maxVersionHistory:]
		}
		r.versions[s.Manifest.Name] = hist
	}
	r.skills[s.Manifest.Name] = s
	return nil
}

// Rollback restores the most recent prior version of a skill, returning an
// error when there is nothing to roll back to.
func (r *Registry) Rollback(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	hist := r.versions[name]
	if len(hist) == 0 {
		return fmt.Errorf("skill %q has no prior version to roll back to", name)
	}
	prev := hist[len(hist)-1]
	r.versions[name] = hist[:len(hist)-1]
	r.skills[name] = prev
	return nil
}

// VersionCount returns how many prior (rollback-able) versions a skill has.
func (r *Registry) VersionCount(name string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.versions[name])
}

// Install loads a skill from a directory and adds it to the registry.
func (r *Registry) Install(dir string) error {
	skill, err := LoadSkill(dir)
	if err != nil {
		return fmt.Errorf("install skill from %s: %w", dir, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.skills[skill.Manifest.Name]; exists {
		return fmt.Errorf("skill %q already installed", skill.Manifest.Name)
	}

	r.skills[skill.Manifest.Name] = skill
	return nil
}

// Get returns an installed skill by name.
func (r *Registry) Get(name string) (*InstalledSkill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	return s, ok
}

// List returns all installed, enabled skill names sorted
// alphabetically. Disabled skills are skipped so the agent's
// skill_router doesn't surface them. Use ListAll for an
// admin-style view that includes disabled.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.skills))
	for name, s := range r.skills {
		if s.Disabled {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ListAll returns every installed skill including those the curator
// has disabled. Admin / management UIs use this so disabled skills
// remain discoverable for re-enable.
func (r *Registry) ListAll() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Disable marks a skill as disabled. Idempotent in spirit but
// returns an error containing "already disabled" so the curator's
// prune loop can distinguish a first-time disable from a retry.
func (r *Registry) Disable(name, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.skills[name]
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	if s.Disabled {
		return fmt.Errorf("skill %q already disabled", name)
	}
	s.Disabled = true
	s.DisabledReason = reason
	return nil
}

// Enable clears the disabled flag on a skill. Returns an error if
// the skill is unknown; succeeds (no-op) if it's already enabled.
func (r *Registry) Enable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.skills[name]
	if !ok {
		return fmt.Errorf("skill %q not found", name)
	}
	s.Disabled = false
	s.DisabledReason = ""
	return nil
}

// IsDisabled reports whether a skill is currently disabled.
func (r *Registry) IsDisabled(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	if !ok {
		return false
	}
	return s.Disabled
}

// Uninstall removes a skill from the registry.
func (r *Registry) Uninstall(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skills[name]; !exists {
		return fmt.Errorf("skill %q not found", name)
	}
	delete(r.skills, name)
	return nil
}

// InstallDirect adds a pre-loaded skill to the registry.
func (r *Registry) InstallDirect(s *InstalledSkill) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skills[s.Manifest.Name]; exists {
		return fmt.Errorf("skill %q already installed", s.Manifest.Name)
	}
	r.skills[s.Manifest.Name] = s
	return nil
}

// GetInstructions returns the instructions (skill.md content) for the given skill names.
// Returns a map of name -> instructions. Missing or disabled skills are skipped so the
// agent's skill_router cannot accidentally load a skill the curator has pruned.
func (r *Registry) GetInstructions(names []string) map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string)
	for _, name := range names {
		if s, ok := r.skills[name]; ok && !s.Disabled {
			result[name] = s.Instructions
		}
	}
	return result
}
