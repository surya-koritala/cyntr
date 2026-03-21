package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	agenttools "github.com/cyntr-dev/cyntr/modules/agent/tools"
)

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
	// F2: If file_path is set and content is empty, read the file
	if body.Content == "" && body.FilePath != "" {
		data, err := os.ReadFile(body.FilePath)
		if err != nil {
			RespondError(w, 400, "FILE_ERROR", err.Error())
			return
		}
		body.Content = string(data)
		if body.Title == "" {
			body.Title = filepath.Base(body.FilePath)
		}
	}
	id := fmt.Sprintf("kb_%d", time.Now().UnixNano())
	if err := knowledgeTool.Ingest(id, body.Title, body.Content, body.Tags); err != nil {
		RespondError(w, 500, "INGEST_FAILED", err.Error())
		return
	}
	Respond(w, 201, map[string]string{"status": "ingested", "id": id})
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
