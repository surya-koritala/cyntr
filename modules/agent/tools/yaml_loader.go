package tools

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadToolsFromDir scans a directory for *.yaml tool definitions and returns them.
func LoadToolsFromDir(dir string) ([]*YAMLTool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // directory doesn't exist, no tools to load
		}
		return nil, fmt.Errorf("read tools dir: %w", err)
	}

	var tools []*YAMLTool
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		var def YAMLToolDef
		if err := yaml.Unmarshal(data, &def); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}

		tool, err := NewYAMLTool(def)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", path, err)
		}

		tools = append(tools, tool)
	}

	return tools, nil
}
