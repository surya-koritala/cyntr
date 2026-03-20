package skill

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadSkill loads a skill from a directory containing skill.yaml and optionally skill.md and SIGNATURE.
func LoadSkill(dir string) (*InstalledSkill, error) {
	// Read manifest
	manifestPath := filepath.Join(dir, "skill.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	var manifest SkillManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}

	if err := manifest.Validate(); err != nil {
		return nil, err
	}

	skill := &InstalledSkill{
		Manifest: manifest,
		Path:     dir,
	}

	// Read instructions (optional)
	instructionsPath := filepath.Join(dir, "skill.md")
	if instrData, err := os.ReadFile(instructionsPath); err == nil {
		skill.Instructions = string(instrData)
	}

	// Read signature (optional)
	sigPath := filepath.Join(dir, "SIGNATURE")
	if sigData, err := os.ReadFile(sigPath); err == nil {
		skill.Signature = string(sigData)
	}

	return skill, nil
}
