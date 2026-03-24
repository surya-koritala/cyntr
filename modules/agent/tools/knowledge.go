package tools

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
	_ "modernc.org/sqlite"
)

type KnowledgeTool struct {
	mu       sync.Mutex
	db       *sql.DB
	embedder *SimpleEmbedding
}

func NewKnowledgeTool(dbPath string) (*KnowledgeTool, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open knowledge db: %w", err)
	}
	db.Exec("PRAGMA journal_mode=WAL")

	_, err = db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS knowledge USING fts5(id, title, content, tags)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create knowledge table: %w", err)
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS knowledge_meta (
		doc_id TEXT PRIMARY KEY,
		title TEXT,
		source_url TEXT,
		tags TEXT,
		created_at TEXT
	)`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("create knowledge_meta table: %w", err)
	}

	tool := &KnowledgeTool{db: db, embedder: NewSimpleEmbedding()}

	// Build initial embeddings from existing data
	go tool.rebuildEmbeddings()

	return tool, nil
}

func (t *KnowledgeTool) Name() string { return "knowledge_search" }
func (t *KnowledgeTool) Description() string {
	return "Search a local knowledge base of documents for relevant information. Use this when the user asks about internal documentation, policies, or procedures."
}
func (t *KnowledgeTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"query": {Type: "string", Description: "Search query", Required: true},
		"tags":  {Type: "string", Description: "Comma-separated tags to filter results", Required: false},
		"mode":  {Type: "string", Description: "Search mode: keyword (default) or semantic", Required: false},
	}
}

func (t *KnowledgeTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	query := input["query"]
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	// Semantic search mode
	mode := input["mode"]
	filterTags := input["tags"]
	if mode == "semantic" && t.embedder != nil && len(t.embedder.vocabulary) > 0 {
		return t.semanticSearch(query, filterTags)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	rows, err := t.db.QueryContext(ctx,
		`SELECT title, content, tags FROM knowledge WHERE knowledge MATCH ? ORDER BY rank LIMIT 5`,
		query,
	)
	if err != nil {
		return "", fmt.Errorf("search: %w", err)
	}
	defer rows.Close()

	type result struct {
		title, content, tags string
	}
	var rawResults []result
	for rows.Next() {
		var r result
		rows.Scan(&r.title, &r.content, &r.tags)
		if len(r.content) > 500 {
			r.content = r.content[:500] + "..."
		}
		rawResults = append(rawResults, r)
	}

	// F4: Tag-based filtering
	if filterTags != "" {
		tagList := strings.Split(filterTags, ",")
		var filtered []result
		for _, r := range rawResults {
			for _, tag := range tagList {
				if strings.Contains(r.tags, strings.TrimSpace(tag)) {
					filtered = append(filtered, r)
					break
				}
			}
		}
		rawResults = filtered
	}

	var results []string
	for _, r := range rawResults {
		results = append(results, fmt.Sprintf("**%s**\n%s\nTags: %s", r.title, r.content, r.tags))
	}

	if len(results) == 0 {
		return "No documents found matching: " + query, nil
	}

	return strings.Join(results, "\n\n---\n\n"), nil
}

// ChunkDocument splits content into chunks with paragraph-aware boundaries.
func ChunkDocument(content string, chunkSize, overlap int) []string {
	if len(content) <= chunkSize {
		return []string{content}
	}
	var chunks []string
	paras := strings.Split(content, "\n\n")
	var current strings.Builder

	for _, para := range paras {
		if current.Len()+len(para) > chunkSize && current.Len() > 0 {
			chunks = append(chunks, current.String())
			// Keep overlap from end of previous chunk
			text := current.String()
			current.Reset()
			if overlap > 0 && len(text) > overlap {
				current.WriteString(text[len(text)-overlap:])
				current.WriteString("\n\n")
			}
		}
		current.WriteString(para)
		current.WriteString("\n\n")
	}
	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}
	return chunks
}

// Ingest adds a document to the knowledge base with smart chunking.
func (t *KnowledgeTool) Ingest(id, title, content, tags string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	chunks := ChunkDocument(content, 500, 100)
	for i, chunk := range chunks {
		chunkID := fmt.Sprintf("%s_chunk_%d", id, i)
		_, err := t.db.Exec(
			`INSERT OR REPLACE INTO knowledge(id, title, content, tags) VALUES (?, ?, ?, ?)`,
			chunkID, title, chunk, tags,
		)
		if err != nil {
			return err
		}
	}

	// F3: Write metadata for source tracking
	t.db.Exec(`INSERT OR REPLACE INTO knowledge_meta(doc_id, title, source_url, tags, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, title, "", tags, time.Now().UTC().Format(time.RFC3339))

	// Rebuild embeddings for semantic search
	go t.rebuildEmbeddings()

	return nil
}

func (t *KnowledgeTool) rebuildEmbeddings() {
	t.mu.Lock()
	defer t.mu.Unlock()

	rows, err := t.db.Query("SELECT content FROM knowledge")
	if err != nil {
		return
	}
	defer rows.Close()

	var docs []string
	for rows.Next() {
		var content string
		rows.Scan(&content)
		docs = append(docs, content)
	}
	if len(docs) > 0 {
		t.embedder.BuildVocabulary(docs)
	}
}

func (t *KnowledgeTool) semanticSearch(query string, filterTags string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	queryVec := t.embedder.Embed(query)
	if queryVec == nil {
		return "Semantic search not available (no documents indexed).", nil
	}

	rows, err := t.db.Query("SELECT id, title, content, tags FROM knowledge")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type scored struct {
		title, content, tags string
		score                float64
	}
	var results []scored

	for rows.Next() {
		var id, title, content, tags string
		rows.Scan(&id, &title, &content, &tags)

		// Filter by tags if provided
		if filterTags != "" {
			matched := false
			for _, tag := range strings.Split(filterTags, ",") {
				if strings.Contains(strings.ToLower(tags), strings.TrimSpace(strings.ToLower(tag))) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		docVec := t.embedder.Embed(content)
		sim := CosineSimilarity(queryVec, docVec)
		if sim > 0.01 { // minimum relevance threshold
			results = append(results, scored{title: title, content: content, tags: tags, score: sim})
		}
	}

	// Sort by score descending
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if len(results) == 0 {
		return "No semantically similar documents found for: " + query, nil
	}

	// Return top 5
	if len(results) > 5 {
		results = results[:5]
	}

	var output []string
	for _, r := range results {
		content := r.content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		output = append(output, fmt.Sprintf("**%s** (relevance: %.0f%%)\n%s\nTags: %s", r.title, r.score*100, content, r.tags))
	}

	return strings.Join(output, "\n\n---\n\n"), nil
}

// Delete removes a document and all its chunks from the knowledge base.
func (t *KnowledgeTool) Delete(id string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	// Delete exact ID and all chunks (id_chunk_0, id_chunk_1, etc.)
	t.db.Exec(`DELETE FROM knowledge WHERE id = ? OR id LIKE ?`, id, id+"_chunk_%")
	t.db.Exec(`DELETE FROM knowledge_meta WHERE doc_id = ?`, id)
	return nil
}

// List returns all document IDs and titles from the metadata table.
func (t *KnowledgeTool) List() ([]map[string]string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	rows, err := t.db.Query(`SELECT doc_id, title, tags, source_url, created_at FROM knowledge_meta ORDER BY title`)
	if err != nil {
		// Fallback to legacy table if knowledge_meta is empty or missing
		rows, err = t.db.Query(`SELECT id, title, tags FROM knowledge ORDER BY title`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var docs []map[string]string
		for rows.Next() {
			var id, title, tags string
			rows.Scan(&id, &title, &tags)
			docs = append(docs, map[string]string{"id": id, "title": title, "tags": tags})
		}
		if docs == nil {
			docs = []map[string]string{}
		}
		return docs, nil
	}
	defer rows.Close()
	var docs []map[string]string
	for rows.Next() {
		var id, title, tags, sourceURL, createdAt string
		rows.Scan(&id, &title, &tags, &sourceURL, &createdAt)
		docs = append(docs, map[string]string{
			"id":         id,
			"title":      title,
			"tags":       tags,
			"source_url": sourceURL,
			"created_at": createdAt,
		})
	}
	if docs == nil {
		docs = []map[string]string{}
	}
	return docs, nil
}

func (t *KnowledgeTool) Close() error { return t.db.Close() }

// RunbookTool wraps KnowledgeTool to search specifically for runbook entries.
type RunbookTool struct {
	kb *KnowledgeTool
}

func NewRunbookTool(kb *KnowledgeTool) *RunbookTool {
	return &RunbookTool{kb: kb}
}

func (t *RunbookTool) Name() string { return "runbook_search" }
func (t *RunbookTool) Description() string {
	return "Search runbooks for diagnostic and troubleshooting procedures. Returns step-by-step instructions for known issues."
}
func (t *RunbookTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"query": {Type: "string", Description: "Issue or symptom to search runbooks for", Required: true},
	}
}

func (t *RunbookTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	// Search knowledge base with runbook tag filter
	input["tags"] = "runbook"
	return t.kb.Execute(ctx, input)
}
