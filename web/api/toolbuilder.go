package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// toolDef mirrors the YAML tool definition for the builder API.
type toolDef struct {
	Name        string                    `json:"name" yaml:"name"`
	Description string                    `json:"description" yaml:"description"`
	Command     string                    `json:"command" yaml:"command"`
	Timeout     string                    `json:"timeout" yaml:"timeout"`
	Parameters  map[string]toolParamDef   `json:"parameters" yaml:"parameters"`
}

// toolParamDef defines a parameter in the tool definition.
type toolParamDef struct {
	Type        string `json:"type" yaml:"type"`
	Description string `json:"description" yaml:"description"`
	Required    bool   `json:"required" yaml:"required"`
}

const toolsDir = "tools"

func (s *Server) handleToolList(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		if os.IsNotExist(err) {
			Respond(w, 200, []toolDef{})
			return
		}
		RespondError(w, 500, "TOOL_READ_ERROR", err.Error())
		return
	}

	var tools []toolDef
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(toolsDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			RespondError(w, 500, "TOOL_READ_ERROR", fmt.Sprintf("read %s: %v", entry.Name(), err))
			return
		}

		var def toolDef
		if err := yaml.Unmarshal(data, &def); err != nil {
			RespondError(w, 500, "TOOL_PARSE_ERROR", fmt.Sprintf("parse %s: %v", entry.Name(), err))
			return
		}

		tools = append(tools, def)
	}

	if tools == nil {
		tools = []toolDef{}
	}
	Respond(w, 200, tools)
}

func (s *Server) handleToolCreate(w http.ResponseWriter, r *http.Request) {
	var body toolDef
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}

	if strings.TrimSpace(body.Name) == "" {
		RespondError(w, 400, "VALIDATION_ERROR", "name is required")
		return
	}

	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		RespondError(w, 500, "FS_ERROR", fmt.Sprintf("create tools directory: %v", err))
		return
	}

	data, err := yaml.Marshal(&body)
	if err != nil {
		RespondError(w, 500, "MARSHAL_ERROR", err.Error())
		return
	}

	path := filepath.Join(toolsDir, body.Name+".yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		RespondError(w, 500, "WRITE_ERROR", err.Error())
		return
	}

	Respond(w, 201, map[string]string{"status": "created", "name": body.Name})
}

func (s *Server) handleToolDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		RespondError(w, 400, "VALIDATION_ERROR", "tool name is required")
		return
	}

	path := filepath.Join(toolsDir, name+".yaml")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			RespondError(w, 404, "NOT_FOUND", fmt.Sprintf("tool %q not found", name))
			return
		}
		RespondError(w, 500, "DELETE_ERROR", err.Error())
		return
	}

	Respond(w, 200, map[string]string{"status": "deleted", "name": name})
}
