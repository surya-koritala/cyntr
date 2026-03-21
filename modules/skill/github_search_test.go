package skill

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubSearcherSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"name":           "my-cyntr-skill",
					"full_name":      "user/my-cyntr-skill",
					"description":    "A cool skill",
					"owner":          map[string]string{"login": "user"},
					"default_branch": "main",
				},
			},
		})
	}))
	defer server.Close()

	g := NewGitHubSearcher()
	g.SetAPIURL(server.URL)

	results, err := g.Search(context.Background(), "cool")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "my-cyntr-skill" {
		t.Fatalf("expected my-cyntr-skill, got %q", results[0].Name)
	}
	if results[0].Author != "user" {
		t.Fatalf("expected author user, got %q", results[0].Author)
	}
}

func TestGitHubSearcherEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer server.Close()

	g := NewGitHubSearcher()
	g.SetAPIURL(server.URL)

	results, err := g.Search(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestGitHubSearcherAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"message":"rate limit exceeded"}`))
	}))
	defer server.Close()

	g := NewGitHubSearcher()
	g.SetAPIURL(server.URL)

	_, err := g.Search(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error on 403")
	}
}
