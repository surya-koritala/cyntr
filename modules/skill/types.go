package skill

import "fmt"

// SkillManifest is parsed from skill.yaml.
type SkillManifest struct {
	Name         string       `yaml:"name"`
	Version      string       `yaml:"version"`
	Author       string       `yaml:"author"`
	License      string       `yaml:"license"`
	Capabilities Capabilities `yaml:"capabilities"`
	Signing      SigningInfo   `yaml:"signing"`
}

// Validate checks required fields.
func (m SkillManifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("skill manifest: name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("skill manifest: version is required")
	}
	return nil
}

// Capabilities declares what a skill is allowed to access.
type Capabilities struct {
	Network    []string `yaml:"network"`    // URL patterns: "https://*.example.com"
	Filesystem []string `yaml:"filesystem"` // path patterns, or empty for none
	Shell      bool     `yaml:"shell"`
	Tools      []string `yaml:"tools"` // tool names the skill can use
}

// HasNetwork returns true if the skill declares any network access.
func (c Capabilities) HasNetwork() bool {
	return len(c.Network) > 0
}

// HasFilesystem returns true if the skill declares any filesystem access.
func (c Capabilities) HasFilesystem() bool {
	return len(c.Filesystem) > 0
}

// SigningInfo holds signature metadata.
type SigningInfo struct {
	Registry    string `yaml:"registry"`
	Fingerprint string `yaml:"fingerprint"`
}

// InstalledSkill represents a loaded and validated skill.
type InstalledSkill struct {
	Manifest     SkillManifest
	Instructions string // contents of skill.md
	Path         string // directory path on disk
	Signature    string // contents of SIGNATURE file
}
