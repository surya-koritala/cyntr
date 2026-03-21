package tools

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/cyntr-dev/cyntr/modules/agent"
	_ "modernc.org/sqlite"
)

type KnowledgeTool struct {
	mu sync.Mutex
	db *sql.DB
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

	return &KnowledgeTool{db: db}, nil
}

func (t *KnowledgeTool) Name() string { return "knowledge_search" }
func (t *KnowledgeTool) Description() string {
	return "Search a local knowledge base of documents for relevant information. Use this when the user asks about internal documentation, policies, or procedures."
}
func (t *KnowledgeTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"query": {Type: "string", Description: "Search query", Required: true},
	}
}

func (t *KnowledgeTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	query := input["query"]
	if query == "" {
		return "", fmt.Errorf("query is required")
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

	var results []string
	for rows.Next() {
		var title, content, tags string
		rows.Scan(&title, &content, &tags)
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		results = append(results, fmt.Sprintf("**%s**\n%s\nTags: %s", title, content, tags))
	}

	if len(results) == 0 {
		return "No documents found matching: " + query, nil
	}

	return strings.Join(results, "\n\n---\n\n"), nil
}

// Ingest adds a document to the knowledge base.
func (t *KnowledgeTool) Ingest(id, title, content, tags string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := t.db.Exec(
		`INSERT OR REPLACE INTO knowledge(id, title, content, tags) VALUES (?, ?, ?, ?)`,
		id, title, content, tags,
	)
	return err
}

// Delete removes a document from the knowledge base.
func (t *KnowledgeTool) Delete(id string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, err := t.db.Exec(`DELETE FROM knowledge WHERE id = ?`, id)
	return err
}

// List returns all document IDs and titles.
func (t *KnowledgeTool) List() ([]map[string]string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	rows, err := t.db.Query(`SELECT id, title, tags FROM knowledge ORDER BY title`)
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

func (t *KnowledgeTool) Close() error { return t.db.Close() }
