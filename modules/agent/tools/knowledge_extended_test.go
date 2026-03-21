package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestChunkDocumentShort(t *testing.T) {
	chunks := ChunkDocument("short text", 500, 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for short text, got %d", len(chunks))
	}
	if chunks[0] != "short text" {
		t.Fatalf("expected exact text back, got %q", chunks[0])
	}
}

func TestChunkDocumentLong(t *testing.T) {
	// Create a document with multiple paragraphs
	paras := make([]string, 20)
	for i := range paras {
		paras[i] = "This is paragraph number " + strings.Repeat("word ", 20)
	}
	doc := strings.Join(paras, "\n\n")

	chunks := ChunkDocument(doc, 500, 100)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	// Each chunk should be <= ~600 chars (500 + overlap margin)
	for i, c := range chunks {
		if len(c) > 700 {
			t.Fatalf("chunk %d too large: %d chars", i, len(c))
		}
	}
}

func TestChunkDocumentOverlap(t *testing.T) {
	doc := "AAAA\n\nBBBB\n\nCCCC\n\nDDDD"
	chunks := ChunkDocument(doc, 10, 5)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}

func TestChunkDocumentEmptyContent(t *testing.T) {
	chunks := ChunkDocument("", 500, 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for empty text, got %d", len(chunks))
	}
}

func TestChunkDocumentExactBoundary(t *testing.T) {
	// Content exactly at chunk size boundary
	content := strings.Repeat("a", 500)
	chunks := ChunkDocument(content, 500, 100)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for exact-boundary text, got %d", len(chunks))
	}
}

func TestChunkDocumentZeroOverlap(t *testing.T) {
	paras := []string{"First paragraph here.", "Second paragraph here.", "Third paragraph here."}
	doc := strings.Join(paras, "\n\n")
	chunks := ChunkDocument(doc, 30, 0)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks with zero overlap, got %d", len(chunks))
	}
}

func TestKnowledgeToolTagFiltering(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	kt.Ingest("d1", "AWS Guide", "How to use AWS EC2 instances", "aws,cloud")
	kt.Ingest("d2", "Python Guide", "How to write Python code", "python,dev")
	kt.Ingest("d3", "Azure Guide", "How to use Azure VMs", "azure,cloud")

	// Search with tag filter
	result, err := kt.Execute(context.Background(), map[string]string{"query": "guide", "tags": "cloud"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(result, "AWS") || !strings.Contains(result, "Azure") {
		t.Fatalf("expected cloud results, got: %s", result)
	}
	if strings.Contains(result, "Python") {
		t.Fatal("should not include Python (not tagged cloud)")
	}
}

func TestKnowledgeToolTagFilteringNoMatch(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	kt.Ingest("d1", "AWS Guide", "How to use AWS EC2 instances", "aws,cloud")

	// Search with a tag that does not match any document
	result, err := kt.Execute(context.Background(), map[string]string{"query": "AWS", "tags": "nonexistent"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(result, "No documents found") {
		t.Fatalf("expected no results with non-matching tag, got: %s", result)
	}
}

func TestKnowledgeToolSourceURL(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	kt.Ingest("d1", "Doc", "content", "tag")
	docs, err := kt.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}
	// Should have created_at from knowledge_meta
	if docs[0]["created_at"] == "" {
		t.Fatal("expected created_at to be set in knowledge_meta")
	}
	if docs[0]["doc_id"] != "" && docs[0]["id"] == "" {
		// Metadata table uses doc_id
	}
}

func TestKnowledgeToolEmptyQuery(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	_, err = kt.Execute(context.Background(), map[string]string{"query": ""})
	if err == nil {
		t.Fatal("expected error for empty query")
	}
}

func TestKnowledgeToolIngestLongContent(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	// Ingest content that will be chunked
	longContent := strings.Repeat("This is a paragraph of text. ", 100) + "\n\n" + strings.Repeat("Another paragraph. ", 100)
	err = kt.Ingest("long1", "Long Doc", longContent, "test")
	if err != nil {
		t.Fatalf("ingest long content: %v", err)
	}

	// Should still be searchable
	result, err := kt.Execute(context.Background(), map[string]string{"query": "paragraph"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if strings.Contains(result, "No documents found") {
		t.Fatal("expected results for chunked document")
	}
}

func TestKnowledgeToolDeleteRemovesChunks(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	// Ingest content that will produce multiple chunks
	longContent := strings.Repeat("Chunk content here. ", 50) + "\n\n" + strings.Repeat("More chunk content. ", 50)
	kt.Ingest("chunked1", "Chunked Doc", longContent, "test")

	// Verify it was ingested
	docs, _ := kt.List()
	if len(docs) != 1 {
		t.Fatalf("expected 1 doc, got %d", len(docs))
	}

	// Delete and verify all chunks are removed
	kt.Delete("chunked1")
	docs, _ = kt.List()
	if len(docs) != 0 {
		t.Fatalf("expected 0 docs after delete, got %d", len(docs))
	}

	// Search should return nothing
	result, _ := kt.Execute(context.Background(), map[string]string{"query": "chunk content"})
	if !strings.Contains(result, "No documents found") {
		t.Fatalf("expected no results after delete, got: %s", result)
	}
}

func TestRunbookToolName(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	rt := NewRunbookTool(kt)
	if rt.Name() != "runbook_search" {
		t.Fatalf("expected runbook_search, got %q", rt.Name())
	}
}

func TestRunbookToolDescription(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	rt := NewRunbookTool(kt)
	desc := rt.Description()
	if desc == "" {
		t.Fatal("expected non-empty description")
	}
	if !strings.Contains(desc, "runbook") {
		t.Fatalf("expected description to mention runbook, got %q", desc)
	}
}

func TestRunbookToolParameters(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	rt := NewRunbookTool(kt)
	params := rt.Parameters()
	if _, ok := params["query"]; !ok {
		t.Fatal("expected query parameter")
	}
}

func TestRunbookToolSearchesWithTag(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	kt.Ingest("rb1", "Restart Service", "Step 1: Stop. Step 2: Start.", "runbook")
	kt.Ingest("doc1", "General Info", "Not a runbook", "info")

	rt := NewRunbookTool(kt)
	result, err := rt.Execute(context.Background(), map[string]string{"query": "restart"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(result, "Restart") {
		t.Fatalf("expected runbook result, got: %s", result)
	}
}

func TestRunbookToolExcludesNonRunbook(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	kt.Ingest("rb1", "Restart Service", "Step 1: Stop the service. Step 2: Start the service.", "runbook")
	kt.Ingest("doc1", "Service Overview", "General service overview and documentation", "info")

	rt := NewRunbookTool(kt)
	result, err := rt.Execute(context.Background(), map[string]string{"query": "service"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	// Should include runbook-tagged content
	if !strings.Contains(result, "Restart") {
		t.Fatalf("expected runbook result, got: %s", result)
	}
	// Should not include non-runbook content
	if strings.Contains(result, "Overview") {
		t.Fatal("should not include non-runbook tagged document")
	}
}

func TestKnowledgeToolContentTruncation(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	// Ingest a single short chunk that is still over 500 chars in content
	// Since ChunkDocument uses 500 chunk size, we need content that fits in one chunk
	// but when returned from Execute, gets truncated at 500 chars
	shortButLong := strings.Repeat("x", 600)
	kt.Ingest("trunc1", "Truncated Doc", shortButLong, "test")

	result, err := kt.Execute(context.Background(), map[string]string{"query": "Truncated"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(result, "...") {
		t.Fatal("expected content to be truncated with ellipsis")
	}
}

func TestKnowledgeToolMultipleTagFilter(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()

	kt.Ingest("d1", "AWS Guide", "How to use AWS EC2 instances", "aws,cloud")
	kt.Ingest("d2", "Python Guide", "How to write Python code", "python,dev")
	kt.Ingest("d3", "Go Guide", "How to write Go code", "go,dev")

	// Filter by multiple tags (comma-separated) -- should match python,dev and go,dev
	result, err := kt.Execute(context.Background(), map[string]string{"query": "guide", "tags": "dev"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(result, "Python") || !strings.Contains(result, "Go") {
		t.Fatalf("expected dev-tagged results, got: %s", result)
	}
	if strings.Contains(result, "AWS") {
		t.Fatal("should not include AWS (not tagged dev)")
	}
}
