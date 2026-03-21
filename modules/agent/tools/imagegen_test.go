package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestImageGenToolName(t *testing.T) {
	if NewImageGenTool().Name() != "generate_image" {
		t.Fatal("unexpected name")
	}
}

func TestImageGenToolExecute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatal("missing auth")
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["prompt"] != "a sunset over mountains" {
			t.Fatalf("unexpected prompt: %v", req["prompt"])
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{
				{"url": "https://example.com/image.png", "revised_prompt": "A beautiful sunset over mountains with warm colors"},
			},
		})
	}))
	defer server.Close()

	tool := NewImageGenTool()
	tool.SetAPIURL(server.URL)

	result, err := tool.Execute(context.Background(), map[string]string{
		"prompt": "a sunset over mountains", "api_key": "test-key",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "https://example.com/image.png") {
		t.Fatalf("expected image URL, got %q", result)
	}
	if !strings.Contains(result, "Revised Prompt:") {
		t.Fatalf("expected revised prompt, got %q", result)
	}
}

func TestImageGenToolMissingParams(t *testing.T) {
	tool := NewImageGenTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing params")
	}
}

func TestImageGenToolAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"invalid prompt"}}`)
	}))
	defer server.Close()

	tool := NewImageGenTool()
	tool.SetAPIURL(server.URL)
	_, err := tool.Execute(context.Background(), map[string]string{
		"prompt": "test", "api_key": "key",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestImageGenToolCustomSize(t *testing.T) {
	var receivedSize string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		receivedSize = req["size"].(string)
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]string{{"url": "https://example.com/img.png", "revised_prompt": "test"}},
		})
	}))
	defer server.Close()

	tool := NewImageGenTool()
	tool.SetAPIURL(server.URL)
	tool.Execute(context.Background(), map[string]string{
		"prompt": "test", "api_key": "key", "size": "512x512",
	})
	if receivedSize != "512x512" {
		t.Fatalf("expected 512x512, got %q", receivedSize)
	}
}

func TestImageGenToolNoData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer server.Close()

	tool := NewImageGenTool()
	tool.SetAPIURL(server.URL)
	_, err := tool.Execute(context.Background(), map[string]string{
		"prompt": "test", "api_key": "key",
	})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}
