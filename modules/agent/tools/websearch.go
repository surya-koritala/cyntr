package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type WebSearchTool struct {
	client       *http.Client
	firecrawlURL string
}

func NewWebSearchTool() *WebSearchTool {
	fcURL := os.Getenv("FIRECRAWL_URL")
	if fcURL == "" {
		fcURL = "http://localhost:3002"
	}
	return &WebSearchTool{
		client:       &http.Client{Timeout: 30 * time.Second},
		firecrawlURL: fcURL,
	}
}

func (t *WebSearchTool) SetAPIURL(url string) { t.firecrawlURL = url }

func (t *WebSearchTool) Name() string { return "web_search" }
func (t *WebSearchTool) Description() string {
	return "Search the web and return titles, URLs, descriptions, and article content. Powered by Firecrawl — works on any site."
}
func (t *WebSearchTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"query":       {Type: "string", Description: "Search query", Required: true},
		"num_results": {Type: "string", Description: "Number of results (1-10, default 5)", Required: false},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	query := input["query"]
	if query == "" {
		return "", fmt.Errorf("query is required")
	}

	numResults := 5
	if n := input["num_results"]; n != "" {
		if parsed, err := strconv.Atoi(n); err == nil && parsed >= 1 && parsed <= 10 {
			numResults = parsed
		}
	}

	// Call Firecrawl search
	payload, _ := json.Marshal(map[string]any{
		"query": query,
		"limit": numResults,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", t.firecrawlURL+"/v1/search", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("search error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Success bool `json:"success"`
		Data    []struct {
			URL         string `json:"url"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Markdown    string `json:"markdown"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if !result.Success || len(result.Data) == 0 {
		return "No results found.", nil
	}

	var output string
	for i, item := range result.Data {
		title := item.Title
		if title == "" {
			title = item.URL
		}
		desc := item.Description
		if desc == "" && len(item.Markdown) > 200 {
			desc = item.Markdown[:200] + "..."
		}
		output += fmt.Sprintf("%d. %s\n   URL: %s\n   %s\n\n", i+1, title, item.URL, desc)
	}
	return output, nil
}
