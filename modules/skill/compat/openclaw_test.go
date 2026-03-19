package compat

import (
	"os"
	"path/filepath"
	"testing"
)

const testSkillMD = `---
name: weather-checker
description: Check the weather for a city
version: 1.2.0
author: community-dev
tools:
  - name: http_request
  - name: json_parse
---

# Weather Checker

You are a weather checking skill. Use the http_request tool to fetch weather data.

## Usage
When asked about weather, call the http_request tool with the appropriate weather API URL.
`

func TestParseOpenClawSkill(t *testing.T) {
	skill, err := ParseOpenClawSkill(testSkillMD, "/skills/weather")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if skill.Manifest.Name != "openclaw-weather-checker" {
		t.Fatalf("expected prefixed name, got %q", skill.Manifest.Name)
	}
	if skill.Manifest.Version != "1.2.0" {
		t.Fatalf("expected 1.2.0, got %q", skill.Manifest.Version)
	}
	if skill.Manifest.Author != "community-dev" {
		t.Fatalf("expected community-dev, got %q", skill.Manifest.Author)
	}
}

func TestParseOpenClawSkillCapabilities(t *testing.T) {
	skill, _ := ParseOpenClawSkill(testSkillMD, "")

	// Restricted: no shell
	if skill.Manifest.Capabilities.Shell {
		t.Fatal("expected no shell access")
	}
	// Restricted: filesystem only /tmp
	if len(skill.Manifest.Capabilities.Filesystem) != 1 || skill.Manifest.Capabilities.Filesystem[0] != "/tmp/**" {
		t.Fatalf("expected /tmp/** only, got %v", skill.Manifest.Capabilities.Filesystem)
	}
	// No network
	if len(skill.Manifest.Capabilities.Network) != 0 {
		t.Fatalf("expected no network, got %v", skill.Manifest.Capabilities.Network)
	}
	// Tools preserved
	if len(skill.Manifest.Capabilities.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(skill.Manifest.Capabilities.Tools))
	}
	if skill.Manifest.Capabilities.Tools[0] != "http_request" {
		t.Fatalf("expected http_request, got %q", skill.Manifest.Capabilities.Tools[0])
	}
}

func TestParseOpenClawSkillInstructions(t *testing.T) {
	skill, _ := ParseOpenClawSkill(testSkillMD, "")
	if skill.Instructions == "" {
		t.Fatal("expected instructions")
	}
	if skill.Instructions[:1] != "#" {
		t.Fatalf("expected markdown heading, got %q", skill.Instructions[:20])
	}
}

func TestParseOpenClawSkillUnsigned(t *testing.T) {
	skill, _ := ParseOpenClawSkill(testSkillMD, "")
	if skill.Signature != "" {
		t.Fatal("expected empty signature (untrusted)")
	}
}

func TestParseOpenClawSkillMissingName(t *testing.T) {
	content := "---\nversion: 1.0.0\n---\nBody"
	_, err := ParseOpenClawSkill(content, "")
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestParseOpenClawSkillMissingFrontmatter(t *testing.T) {
	_, err := ParseOpenClawSkill("Just markdown, no frontmatter", "")
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestParseOpenClawSkillDefaultVersion(t *testing.T) {
	content := "---\nname: test\n---\nBody"
	skill, err := ParseOpenClawSkill(content, "")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if skill.Manifest.Version != "0.0.0" {
		t.Fatalf("expected default version 0.0.0, got %q", skill.Manifest.Version)
	}
}

func TestLoadOpenClawSkillFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	os.WriteFile(path, []byte(testSkillMD), 0644)

	skill, err := LoadOpenClawSkillFromFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if skill.Manifest.Name != "openclaw-weather-checker" {
		t.Fatalf("expected name, got %q", skill.Manifest.Name)
	}
}

func TestSplitFrontmatterUnclosed(t *testing.T) {
	_, _, err := splitFrontmatter("---\nname: test\nno closing")
	if err == nil {
		t.Fatal("expected error for unclosed frontmatter")
	}
}
