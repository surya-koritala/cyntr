package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestAlatirokToolName(t *testing.T) {
	tool := NewAlatirokTool()
	if tool.Name() != "alatirok" {
		t.Fatalf("expected alatirok, got %s", tool.Name())
	}
}

func TestAlatirokToolRequiresAPIKey(t *testing.T) {
	os.Unsetenv("ALATIROK_API_KEY")
	tool := NewAlatirokTool()
	_, err := tool.Execute(context.Background(), map[string]string{"action": "whoami"})
	if err == nil || err.Error() == "" {
		t.Fatal("expected error when API key not set")
	}
}

func TestAlatirokToolWhoami(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/me" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatal("missing auth header")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id": "agent-1", "displayName": "Cyntr", "type": "agent", "trustScore": 4.5,
		})
	}))
	defer srv.Close()

	os.Setenv("ALATIROK_API_KEY", "test-key")
	os.Setenv("ALATIROK_BASE_URL", srv.URL)
	defer os.Unsetenv("ALATIROK_API_KEY")
	defer os.Unsetenv("ALATIROK_BASE_URL")

	tool := NewAlatirokTool()
	result, err := tool.Execute(context.Background(), map[string]string{"action": "whoami"})
	if err != nil {
		t.Fatalf("whoami failed: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result, "Cyntr") {
		t.Fatalf("expected Cyntr in result, got: %s", result)
	}
}

func TestAlatirokToolGetFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/feed" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("sort") != "new" {
			t.Fatalf("expected sort=new, got %s", r.URL.Query().Get("sort"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "p1", "title": "Test Post"}},
			"total": 1, "hasMore": false,
		})
	}))
	defer srv.Close()

	os.Setenv("ALATIROK_API_KEY", "test-key")
	os.Setenv("ALATIROK_BASE_URL", srv.URL)
	defer os.Unsetenv("ALATIROK_API_KEY")
	defer os.Unsetenv("ALATIROK_BASE_URL")

	tool := NewAlatirokTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"action": "get_feed", "sort": "new",
	})
	if err != nil {
		t.Fatalf("get_feed failed: %v", err)
	}
	if !strings.Contains(result, "Test Post") {
		t.Fatalf("expected Test Post in result, got: %s", result)
	}
}

func TestAlatirokToolCreatePost(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/posts" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]any{"id": "new-post-1", "title": received["title"]})
	}))
	defer srv.Close()

	os.Setenv("ALATIROK_API_KEY", "test-key")
	os.Setenv("ALATIROK_BASE_URL", srv.URL)
	defer os.Unsetenv("ALATIROK_API_KEY")
	defer os.Unsetenv("ALATIROK_BASE_URL")

	tool := NewAlatirokTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"action":    "create_post",
		"title":     "Cyntr v1.1.0 Released",
		"body":      "New features: SLA monitoring, webhook integrations...",
		"post_type": "research",
		"tags":      "ai-agents, open-source, cyntr",
	})
	if err != nil {
		t.Fatalf("create_post failed: %v", err)
	}
	if !strings.Contains(result, "new-post-1") {
		t.Fatalf("expected post ID in result, got: %s", result)
	}
	if received["post_type"] != "research" {
		t.Fatalf("expected research, got %v", received["post_type"])
	}
	tags, _ := received["tags"].([]any)
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %v", tags)
	}
}

func TestAlatirokToolSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("q") != "ai agents" {
			t.Fatalf("expected q=ai agents, got %s", r.URL.Query().Get("q"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"id": "p1", "title": "AI Agent Benchmark"}},
			"total": 1,
		})
	}))
	defer srv.Close()

	os.Setenv("ALATIROK_API_KEY", "test-key")
	os.Setenv("ALATIROK_BASE_URL", srv.URL)
	defer os.Unsetenv("ALATIROK_API_KEY")
	defer os.Unsetenv("ALATIROK_BASE_URL")

	tool := NewAlatirokTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"action": "search", "query": "ai agents",
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if !strings.Contains(result, "AI Agent Benchmark") {
		t.Fatalf("expected search result, got: %s", result)
	}
}

func TestAlatirokToolVote(t *testing.T) {
	var received map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		json.NewEncoder(w).Encode(map[string]any{"success": true, "newScore": 42})
	}))
	defer srv.Close()

	os.Setenv("ALATIROK_API_KEY", "test-key")
	os.Setenv("ALATIROK_BASE_URL", srv.URL)
	defer os.Unsetenv("ALATIROK_API_KEY")
	defer os.Unsetenv("ALATIROK_BASE_URL")

	tool := NewAlatirokTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"action": "vote", "post_id": "post-123", "direction": "up",
	})
	if err != nil {
		t.Fatalf("vote failed: %v", err)
	}
	if !strings.Contains(result, "42") {
		t.Fatalf("expected newScore in result, got: %s", result)
	}
	if received["direction"] != "up" {
		t.Fatalf("expected up, got %v", received["direction"])
	}
}

