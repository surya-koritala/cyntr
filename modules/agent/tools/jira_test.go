package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJiraToolName(t *testing.T) {
	if NewJiraTool().Name() != "jira" {
		t.Fatal("wrong name")
	}
}

func TestJiraToolSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issues": []map[string]any{
				{"key": "PROJ-1", "fields": map[string]any{"summary": "Fix login", "status": map[string]string{"name": "Open"}}},
				{"key": "PROJ-2", "fields": map[string]any{"summary": "Add tests", "status": map[string]string{"name": "In Progress"}}},
			},
		})
	}))
	defer server.Close()

	tool := NewJiraTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"action": "search", "base_url": server.URL, "email": "a@b.com", "token": "tok", "query": "project=PROJ",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsStr(result, "PROJ-1") {
		t.Fatalf("missing: %q", result)
	}
	if !containsStr(result, "Fix login") {
		t.Fatalf("missing: %q", result)
	}
}

func TestJiraToolCreateIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"key": "PROJ-42", "self": "https://jira/rest/api/3/issue/42"})
	}))
	defer server.Close()

	tool := NewJiraTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"action": "create_issue", "base_url": server.URL, "email": "a@b.com", "token": "tok",
		"project": "PROJ", "title": "New bug", "body": "Something broke",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsStr(result, "PROJ-42") {
		t.Fatalf("got %q", result)
	}
}

func TestJiraToolAddComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": "1"})
	}))
	defer server.Close()

	tool := NewJiraTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"action": "add_comment", "base_url": server.URL, "email": "a@b.com", "token": "tok",
		"key": "PROJ-1", "body": "Looking into this",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsStr(result, "Comment added") {
		t.Fatalf("got %q", result)
	}
}

func TestJiraToolMissingParams(t *testing.T) {
	tool := NewJiraTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestJiraToolGetIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"key": "PROJ-1", "fields": map[string]any{
				"summary": "Fix bug", "status": map[string]string{"name": "Open"},
				"assignee": map[string]string{"displayName": "Jane"},
			},
		})
	}))
	defer server.Close()

	tool := NewJiraTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"action": "get_issue", "base_url": server.URL, "email": "a@b.com", "token": "tok", "key": "PROJ-1",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsStr(result, "Fix bug") {
		t.Fatalf("got %q", result)
	}
	if !containsStr(result, "Jane") {
		t.Fatalf("got %q", result)
	}
}
