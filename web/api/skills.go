package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/skill"
)

func (s *Server) handleSkillList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "skill_runtime", Topic: "skill.list",
	})
	if err != nil {
		RespondError(w, 500, "SKILL_ERROR", err.Error())
		return
	}

	// skill.list returns []string names — enrich with details from skill.get
	names, ok := resp.Payload.([]string)
	if !ok {
		Respond(w, 200, resp.Payload)
		return
	}

	type skillInfo struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Description string `json:"description"`
		Author      string `json:"author"`
		Source      string `json:"source"`
	}

	var skills []skillInfo
	for _, name := range names {
		info := skillInfo{Name: name, Source: "local"}
		// Try to get full details
		getCtx, getCancel := context.WithTimeout(r.Context(), 1*time.Second)
		getResp, getErr := s.bus.Request(getCtx, ipc.Message{
			Source: "api", Target: "skill_runtime", Topic: "skill.get",
			Payload: name,
		})
		getCancel()
		if getErr == nil && getResp.Payload != nil {
			// InstalledSkill has Manifest with Name, Version, Author
			if sk, ok := getResp.Payload.(*skill.InstalledSkill); ok {
				info.Version = sk.Manifest.Version
				info.Author = sk.Manifest.Author
				if sk.Path == "embedded://catalog" {
					info.Source = "builtin"
				}
			}
		}
		skills = append(skills, info)
	}

	Respond(w, 200, skills)
}

func (s *Server) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "skill_runtime", Topic: "skill.install",
		Payload: body.Path,
	})
	if err != nil {
		RespondError(w, 500, "INSTALL_FAILED", err.Error())
		return
	}

	Respond(w, 201, map[string]string{"status": "installed"})
}

func (s *Server) handleSkillUninstall(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	_, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "skill_runtime", Topic: "skill.uninstall",
		Payload: name,
	})
	if err != nil {
		RespondError(w, 500, "UNINSTALL_FAILED", err.Error())
		return
	}

	Respond(w, 200, map[string]string{"status": "uninstalled", "name": name})
}

func (s *Server) handleSkillMarketplaceSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	// Built-in results (always available, instant)
	builtinResults := skill.SearchBuiltinCatalog(query)

	// GitHub results (best-effort, don't fail if network unavailable)
	var githubResults []skill.MarketplaceEntry
	searcher := skill.NewGitHubSearcher()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if results, err := searcher.Search(ctx, query); err == nil {
		githubResults = results
	}

	// Merge: built-in first, then GitHub (deduplicate by name)
	seen := make(map[string]bool)
	var merged []skill.MarketplaceEntry
	for _, entry := range builtinResults {
		if entry.Source == "" {
			entry.Source = "builtin"
		}
		merged = append(merged, entry)
		seen[entry.Name] = true
	}
	for _, entry := range githubResults {
		if !seen[entry.Name] {
			entry.Source = "github"
			merged = append(merged, entry)
			seen[entry.Name] = true
		}
	}

	Respond(w, 200, merged)
}

func (s *Server) handleSkillMarketplaceInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		DownloadURL string `json:"download_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	if body.DownloadURL == "" {
		RespondError(w, 400, "MISSING_URL", "download_url is required")
		return
	}

	// Handle openclaw: URL scheme (local OpenClaw skill import)
	if strings.HasPrefix(body.DownloadURL, "openclaw:") {
		skillName := strings.TrimPrefix(body.DownloadURL, "openclaw:")
		// Search common OpenClaw skill locations
		searchPaths := []string{
			"/private/tmp/openclaw-skills/" + skillName + "/SKILL.md",
			os.Getenv("HOME") + "/.openclaw/skills/" + skillName + "/SKILL.md",
		}
		var skillPath string
		for _, p := range searchPaths {
			if _, err := os.Stat(p); err == nil {
				skillPath = p
				break
			}
		}
		if skillPath == "" {
			RespondError(w, 404, "NOT_FOUND", "OpenClaw skill '"+skillName+"' not found locally")
			return
		}
		importCtx, importCancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer importCancel()
		resp, err := s.bus.Request(importCtx, ipc.Message{
			Source: "api", Target: "skill_runtime", Topic: "skill.import_openclaw",
			Payload: skillPath,
		})
		if err != nil {
			RespondError(w, 500, "IMPORT_FAILED", err.Error())
			return
		}
		importedName := ""
		if name, ok := resp.Payload.(string); ok {
			importedName = name
		}
		Respond(w, 201, map[string]string{"status": "installed", "name": importedName, "source": "openclaw"})
		return
	}

	// Download the skill from URL
	marketplace := skill.NewMarketplace("")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	entry := skill.MarketplaceEntry{Name: body.Name, DownloadURL: body.DownloadURL}
	skillDir, err := marketplace.Download(ctx, entry, "skills")
	if err != nil {
		RespondError(w, 500, "DOWNLOAD_FAILED", err.Error())
		return
	}

	// Install via IPC
	installCtx, installCancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer installCancel()
	_, err = s.bus.Request(installCtx, ipc.Message{
		Source: "api", Target: "skill_runtime", Topic: "skill.install",
		Payload: skillDir,
	})
	if err != nil {
		RespondError(w, 500, "INSTALL_FAILED", err.Error())
		return
	}

	Respond(w, 201, map[string]string{"status": "installed", "name": body.Name, "source": skillDir})
}

func (s *Server) handleSkillImportOpenClaw(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	resp, err := s.bus.Request(ctx, ipc.Message{
		Source: "api", Target: "skill_runtime", Topic: "skill.import_openclaw",
		Payload: body.Path,
	})
	if err != nil {
		RespondError(w, 500, "IMPORT_FAILED", err.Error())
		return
	}
	Respond(w, 201, map[string]string{"status": "imported", "name": resp.Payload.(string)})
}
