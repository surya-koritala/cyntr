package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubToolName(t *testing.T) {
	if NewGitHubTool().Name() != "github" {
		t.Fatal("wrong name")
	}
}

func TestGitHubToolListPRs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatal("missing auth")
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "title": "Fix bug", "user": map[string]string{"login": "dev1"}, "state": "open"},
			{"number": 2, "title": "Add feature", "user": map[string]string{"login": "dev2"}, "state": "open"},
		})
	}))
	defer server.Close()

	tool := NewGitHubTool()
	tool.SetAPIURL(server.URL)
	result, err := tool.Execute(context.Background(), map[string]string{"action": "list_prs", "repo": "owner/repo", "token": "test-token"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsStr(result, "Fix bug") {
		t.Fatalf("missing PR: %q", result)
	}
	if !containsStr(result, "#2") {
		t.Fatalf("missing PR #2: %q", result)
	}
}

func TestGitHubToolCreateIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatal("expected POST")
		}
		json.NewEncoder(w).Encode(map[string]any{"number": 42, "html_url": "https://github.com/owner/repo/issues/42"})
	}))
	defer server.Close()

	tool := NewGitHubTool()
	tool.SetAPIURL(server.URL)
	result, err := tool.Execute(context.Background(), map[string]string{
		"action": "create_issue", "repo": "owner/repo", "token": "tok", "title": "Bug report", "body": "It's broken",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsStr(result, "#42") {
		t.Fatalf("missing issue number: %q", result)
	}
}

func TestGitHubToolAddComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"id": 1})
	}))
	defer server.Close()

	tool := NewGitHubTool()
	tool.SetAPIURL(server.URL)
	result, err := tool.Execute(context.Background(), map[string]string{
		"action": "add_comment", "repo": "owner/repo", "token": "tok", "number": "1", "body": "LGTM",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !containsStr(result, "Comment added") {
		t.Fatalf("got %q", result)
	}
}

func TestGitHubToolMissingParams(t *testing.T) {
	tool := NewGitHubTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGitHubToolUnknownAction(t *testing.T) {
	tool := NewGitHubTool()
	_, err := tool.Execute(context.Background(), map[string]string{"action": "delete_repo", "repo": "x", "token": "x"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGitHubToolAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	}))
	defer server.Close()

	tool := NewGitHubTool()
	tool.SetAPIURL(server.URL)
	_, err := tool.Execute(context.Background(), map[string]string{"action": "list_prs", "repo": "x/y", "token": "tok"})
	if err == nil {
		t.Fatal("expected error")
	}
}
