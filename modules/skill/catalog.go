package skill

import (
	"embed"
	"fmt"
	"strings"

	"github.com/cyntr-dev/cyntr/kernel/log"
	"gopkg.in/yaml.v3"
)

//go:embed catalog/*.md
var catalogFS embed.FS

var catalogLogger = log.Default().WithModule("skill_catalog")

// LoadEmbeddedCatalog reads all .md files from the embedded catalog directory
// and returns them as InstalledSkill objects.
func LoadEmbeddedCatalog() []*InstalledSkill {
	entries, err := catalogFS.ReadDir("catalog")
	if err != nil {
		catalogLogger.Warn("no embedded catalog found", map[string]any{"error": err.Error()})
		return nil
	}

	var skills []*InstalledSkill
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := catalogFS.ReadFile("catalog/" + entry.Name())
		if err != nil {
			catalogLogger.Warn("read catalog skill failed", map[string]any{"file": entry.Name(), "error": err.Error()})
			continue
		}
		skill, err := parseCatalogSkill(string(data))
		if err != nil {
			catalogLogger.Warn("parse catalog skill failed", map[string]any{"file": entry.Name(), "error": err.Error()})
			continue
		}
		// Least privilege: each catalog skill gets only the capabilities it
		// declares in its own manifest (parsed above). We do NOT blanket-grant
		// shell/network/filesystem to every catalog skill.
		skills = append(skills, skill)
	}
	return skills
}

// parseCatalogSkill parses a SKILL.md-format file (YAML frontmatter + markdown body).
func parseCatalogSkill(content string) (*InstalledSkill, error) {
	// Split frontmatter from body
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return nil, fmt.Errorf("missing YAML frontmatter (expected ---)")
	}

	frontmatter := strings.TrimSpace(parts[1])
	body := strings.TrimSpace(parts[2])

	var meta struct {
		Name         string       `yaml:"name"`
		Description  string       `yaml:"description"`
		Version      string       `yaml:"version"`
		Author       string       `yaml:"author"`
		Capabilities Capabilities `yaml:"capabilities"`
		Tools        []struct {
			Name string `yaml:"name"`
		} `yaml:"tools"`
	}
	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}
	if meta.Name == "" {
		return nil, fmt.Errorf("skill name is required")
	}
	if meta.Version == "" {
		meta.Version = "1.0.0"
	}

	// Capabilities come from the manifest's own `capabilities:` block (least
	// privilege). If a skill also lists `tools:` shorthand, merge those tool
	// names in without overriding declared network/filesystem/shell.
	caps := meta.Capabilities
	for _, t := range meta.Tools {
		caps.Tools = append(caps.Tools, t.Name)
	}

	return &InstalledSkill{
		Manifest: SkillManifest{
			Name:         meta.Name,
			Version:      meta.Version,
			Author:       meta.Author,
			Capabilities: caps,
		},
		Instructions: body,
		Path:         "embedded://catalog",
	}, nil
}
