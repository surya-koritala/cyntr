package skill

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMarketplaceSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]MarketplaceEntry{
			{Name: "weather", Version: "1.0.0", Author: "community", Description: "Weather checker"},
			{Name: "weather-alerts", Version: "2.0.0", Author: "dev", Description: "Weather alerts"},
		})
	}))
	defer server.Close()

	mp := NewMarketplace(server.URL)
	results, err := mp.Search(context.Background(), "weather")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
	if results[0].Name != "weather" {
		t.Fatalf("got %q", results[0].Name)
	}
}

func TestMarketplaceGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(MarketplaceEntry{
			Name: "jira-helper", Version: "1.2.0", Author: "cyntr-official",
			Description: "Jira integration skill", DownloadURL: "https://registry/skills/jira-helper.tar.gz",
		})
	}))
	defer server.Close()

	mp := NewMarketplace(server.URL)
	entry, err := mp.Get(context.Background(), "jira-helper")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if entry.Name != "jira-helper" {
		t.Fatalf("got %q", entry.Name)
	}
	if entry.Version != "1.2.0" {
		t.Fatalf("got %q", entry.Version)
	}
}

func TestMarketplaceGetNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer server.Close()

	mp := NewMarketplace(server.URL)
	_, err := mp.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMarketplaceDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("name: test-skill\nversion: 1.0.0\n"))
	}))
	defer server.Close()

	mp := NewMarketplace("")
	dir := t.TempDir()
	entry := MarketplaceEntry{Name: "test-skill", DownloadURL: server.URL + "/download"}
	path, err := mp.Download(context.Background(), entry, dir)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if path == "" {
		t.Fatal("expected path")
	}
}

func TestMarketplaceList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]MarketplaceEntry{
			{Name: "skill-a"}, {Name: "skill-b"}, {Name: "skill-c"},
		})
	}))
	defer server.Close()

	mp := NewMarketplace(server.URL)
	list, err := mp.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
}

func TestMarketplaceSearchError(t *testing.T) {
	mp := NewMarketplace("http://127.0.0.1:1") // unreachable
	_, err := mp.Search(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error")
	}
}
