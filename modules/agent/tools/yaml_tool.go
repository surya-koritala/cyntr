package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"text/template"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// YAMLToolDef is the YAML structure for defining a tool.
type YAMLToolDef struct {
	Name        string                      `yaml:"name"`
	Description string                      `yaml:"description"`
	Parameters  map[string]YAMLToolParamDef `yaml:"parameters"`
	Command     string                      `yaml:"command"`
	Timeout     string                      `yaml:"timeout"`
}

// YAMLToolParamDef defines a parameter in the YAML tool definition.
type YAMLToolParamDef struct {
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

// YAMLTool implements the Tool interface for YAML-defined tools.
type YAMLTool struct {
	def     YAMLToolDef
	timeout time.Duration
}

// NewYAMLTool creates a tool from a YAML definition.
func NewYAMLTool(def YAMLToolDef) (*YAMLTool, error) {
	if def.Name == "" {
		return nil, fmt.Errorf("YAML tool: name is required")
	}
	if def.Command == "" {
		return nil, fmt.Errorf("YAML tool %q: command is required", def.Name)
	}

	timeout := 30 * time.Second
	if def.Timeout != "" {
		d, err := time.ParseDuration(def.Timeout)
		if err != nil {
			return nil, fmt.Errorf("YAML tool %q: invalid timeout: %w", def.Name, err)
		}
		timeout = d
	}

	return &YAMLTool{def: def, timeout: timeout}, nil
}

func (t *YAMLTool) Name() string        { return t.def.Name }
func (t *YAMLTool) Description() string { return t.def.Description }

func (t *YAMLTool) Parameters() map[string]agent.ToolParam {
	params := make(map[string]agent.ToolParam)
	for name, p := range t.def.Parameters {
		params[name] = agent.ToolParam{
			Type:        p.Type,
			Description: p.Description,
			Required:    p.Required,
		}
	}
	return params
}

func (t *YAMLTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	// Render command template with input parameters
	tmpl, err := template.New("cmd").Parse(t.def.Command)
	if err != nil {
		return "", fmt.Errorf("parse command template: %w", err)
	}

	var cmdBuf bytes.Buffer
	if err := tmpl.Execute(&cmdBuf, input); err != nil {
		return "", fmt.Errorf("render command: %w", err)
	}

	command := cmdBuf.String()

	ctx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	if len(output) > 65536 {
		output = output[:65536] + "\n... (truncated)"
	}

	if err != nil {
		return output, fmt.Errorf("command failed: %w", err)
	}
	return output, nil
}
