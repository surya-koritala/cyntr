package tools

import (
	"context"
	"path/filepath"
	"testing"
)

func TestKnowledgeToolName(t *testing.T) {
	kt, err := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kt.Close()
	if kt.Name() != "knowledge_search" {
		t.Fatal("wrong name")
	}
}

func TestKnowledgeToolIngestAndSearch(t *testing.T) {
	kt, _ := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	defer kt.Close()

	kt.Ingest("doc1", "Deployment Guide", "How to deploy the app to production using Docker and Kubernetes", "devops,deploy")
	kt.Ingest("doc2", "API Reference", "REST API endpoints for user management and authentication", "api,auth")
	kt.Ingest("doc3", "Troubleshooting", "Common errors and how to fix them in production", "support,errors")

	result, err := kt.Execute(context.Background(), map[string]string{"query": "deploy production"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result == "" || result == "No documents found matching: deploy production" {
		t.Fatal("expected results for deploy query")
	}
}

func TestKnowledgeToolNoResults(t *testing.T) {
	kt, _ := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	defer kt.Close()

	result, err := kt.Execute(context.Background(), map[string]string{"query": "nonexistent xyz"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result != "No documents found matching: nonexistent xyz" {
		t.Fatalf("expected no results, got: %s", result)
	}
}

func TestKnowledgeToolList(t *testing.T) {
	kt, _ := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	defer kt.Close()

	kt.Ingest("d1", "Doc One", "content one", "tag1")
	kt.Ingest("d2", "Doc Two", "content two", "tag2")

	docs, err := kt.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs, got %d", len(docs))
	}
}

func TestKnowledgeToolDelete(t *testing.T) {
	kt, _ := NewKnowledgeTool(filepath.Join(t.TempDir(), "kb.db"))
	defer kt.Close()

	kt.Ingest("d1", "Doc One", "content", "tag")
	kt.Delete("d1")

	docs, _ := kt.List()
	if len(docs) != 0 {
		t.Fatalf("expected 0 docs after delete, got %d", len(docs))
	}
}
