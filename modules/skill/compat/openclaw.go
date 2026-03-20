package compat

import (
	"fmt"
	"os"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/skill"
	"gopkg.in/yaml.v3"
)

// OpenClawFrontmatter represents the YAML frontmatter in a SKILL.md file.
type OpenClawFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
	Author      string `yaml:"author"`
	Tools       []struct {
		Name string `yaml:"name"`
	} `yaml:"tools"`
}

// ParseOpenClawSkill parses an OpenClaw SKILL.md file and converts it to a Cyntr InstalledSkill.
// The skill runs in a restricted compatibility sandbox:
// - No shell access
// - Filesystem limited to /tmp
// - No network access (unless explicitly allowlisted later)
// - Treated as untrusted by default
func ParseOpenClawSkill(content string, sourcePath string) (*skill.InstalledSkill, error) {
	frontmatter, instructions, err := splitFrontmatter(content)
	if err != nil {
		return nil, fmt.Errorf("parse OpenClaw skill: %w", err)
	}

	var fm OpenClawFrontmatter
	if err := yaml.Unmarshal([]byte(frontmatter), &fm); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	if fm.Name == "" {
		return nil, fmt.Errorf("OpenClaw skill missing name")
	}
	if fm.Version == "" {
		fm.Version = "0.0.0"
	}

	// Convert tools list to string slice
	var toolNames []string
	for _, t := range fm.Tools {
		toolNames = append(toolNames, t.Name)
	}

	// Build restricted Cyntr manifest
	manifest := skill.SkillManifest{
		Name:    "openclaw-" + fm.Name, // prefix to avoid name collisions
		Version: fm.Version,
		Author:  fm.Author,
		License: "unknown", // OpenClaw skills don't always specify
		Capabilities: skill.Capabilities{
			Network:    []string{},          // no network by default
			Filesystem: []string{"/tmp/**"}, // restricted to /tmp
			Shell:      false,               // no shell access
			Tools:      toolNames,
		},
	}

	return &skill.InstalledSkill{
		Manifest:     manifest,
		Instructions: strings.TrimSpace(instructions),
		Path:         sourcePath,
		Signature:    "", // unsigned — treated as untrusted
	}, nil
}

// LoadOpenClawSkillFromFile reads a SKILL.md file and parses it.
func LoadOpenClawSkillFromFile(path string) (*skill.InstalledSkill, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read SKILL.md: %w", err)
	}
	return ParseOpenClawSkill(string(content), path)
}

// splitFrontmatter splits a SKILL.md into YAML frontmatter and markdown body.
func splitFrontmatter(content string) (string, string, error) {
	content = strings.TrimSpace(content)
	if !strings.HasPrefix(content, "---") {
		return "", "", fmt.Errorf("missing frontmatter delimiter")
	}

	// Find the closing ---
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", "", fmt.Errorf("unclosed frontmatter")
	}

	frontmatter := strings.TrimSpace(rest[:idx])
	body := strings.TrimSpace(rest[idx+4:]) // skip \n---

	return frontmatter, body, nil
}
