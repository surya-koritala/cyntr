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
}

// NewRegistry creates an empty skill registry.
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]*InstalledSkill)}
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

// List returns all installed skill names sorted alphabetically.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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

// GetInstructions returns the instructions (skill.md content) for the given skill names.
// Returns a map of name -> instructions. Missing skills are skipped.
func (r *Registry) GetInstructions(names []string) map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string)
	for _, name := range names {
		if s, ok := r.skills[name]; ok {
			result[name] = s.Instructions
		}
	}
	return result
}
