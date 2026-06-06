package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	agenttools "github.com/cyntr-dev/cyntr/modules/agent/tools"
)

// resolveKnowledgePath confines a caller-supplied file_path to the operator's
// allowed base directory (CYNTR_KNOWLEDGE_BASE_DIR). It returns the cleaned
// absolute path on success, or an error if ingest-by-path is disabled or the
// path escapes the base via traversal/symlink-style tricks.
func resolveKnowledgePath(filePath string) (string, error) {
	base := os.Getenv("CYNTR_KNOWLEDGE_BASE_DIR")
	if base == "" {
		return "", errors.New("server-side file ingest is disabled: set CYNTR_KNOWLEDGE_BASE_DIR to enable")
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", errors.New("invalid knowledge base directory")
	}
	// Join treats filePath as relative to the base and cleans away any ".."
	// segments; an absolute filePath is also re-rooted under the base.
	candidate := filepath.Join(absBase, filepath.Clean("/"+filePath))
	// Defense in depth: ensure the result is still under the base.
	rel, err := filepath.Rel(absBase, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("file_path escapes the allowed base directory")
	}
	return candidate, nil
}

var knowledgeTool *agenttools.KnowledgeTool

func SetKnowledgeTool(kt *agenttools.KnowledgeTool) {
	knowledgeTool = kt
}

func (s *Server) handleKnowledgeList(w http.ResponseWriter, r *http.Request) {
	if knowledgeTool == nil {
		Respond(w, 200, []any{})
		return
	}
	docs, err := knowledgeTool.List()
	if err != nil {
		RespondError(w, 500, "KNOWLEDGE_ERROR", err.Error())
		return
	}
	Respond(w, 200, docs)
}

func (s *Server) handleKnowledgeIngest(w http.ResponseWriter, r *http.Request) {
	if knowledgeTool == nil {
		RespondError(w, 500, "NOT_CONFIGURED", "knowledge base not configured")
		return
	}
	var body struct {
		Title     string `json:"title"`
		Content   string `json:"content"`
		Tags      string `json:"tags"`
		SourceURL string `json:"source_url"`
		FilePath  string `json:"file_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondError(w, 400, "INVALID_REQUEST", "invalid JSON")
		return
	}
	// F2: If file_path is set and content is empty, read the file — but only
	// from within an operator-allowed base directory. Reading an arbitrary
	// server-side path lets a caller exfiltrate secrets (/etc/passwd, keys).
	// The base dir is set via CYNTR_KNOWLEDGE_BASE_DIR; when unset, server-side
	// path ingest is disabled (fail closed).
	if body.Content == "" && body.FilePath != "" {
		resolved, err := resolveKnowledgePath(body.FilePath)
		if err != nil {
			RespondError(w, 403, "FORBIDDEN", err.Error())
			return
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			RespondError(w, 400, "FILE_ERROR", err.Error())
			return
		}
		body.Content = string(data)
		if body.Title == "" {
			body.Title = filepath.Base(resolved)
		}
	}
	id := fmt.Sprintf("kb_%d", time.Now().UnixNano())
	if err := knowledgeTool.Ingest(id, body.Title, body.Content, body.Tags); err != nil {
		RespondError(w, 500, "INGEST_FAILED", err.Error())
		return
	}
	Respond(w, 201, map[string]string{"status": "ingested", "id": id})
}

func (s *Server) handleKnowledgeSearch(w http.ResponseWriter, r *http.Request) {
	if knowledgeTool == nil {
		Respond(w, 200, []any{})
		return
	}
	query := r.URL.Query().Get("q")
	if query == "" {
		RespondError(w, 400, "MISSING_QUERY", "q parameter required")
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "hybrid"
	}
	results, err := knowledgeTool.Execute(r.Context(), map[string]string{
		"action": "search",
		"query":  query,
		"mode":   mode,
	})
	if err != nil {
		RespondError(w, 500, "SEARCH_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"query": query, "mode": mode, "results": results})
}

func (s *Server) handleKnowledgeDelete(w http.ResponseWriter, r *http.Request) {
	if knowledgeTool == nil {
		RespondError(w, 500, "NOT_CONFIGURED", "knowledge base not configured")
		return
	}
	id := r.PathValue("id")
	if err := knowledgeTool.Delete(id); err != nil {
		RespondError(w, 500, "DELETE_FAILED", err.Error())
		return
	}
	Respond(w, 200, map[string]string{"status": "deleted", "id": id})
}
