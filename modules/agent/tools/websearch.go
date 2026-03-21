package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type WebSearchTool struct {
	client *http.Client
	apiURL string
}

func NewWebSearchTool() *WebSearchTool {
	return &WebSearchTool{
		client: &http.Client{Timeout: 15 * time.Second},
		apiURL: "https://www.googleapis.com/customsearch/v1",
	}
}

func (t *WebSearchTool) SetAPIURL(url string) { t.apiURL = url }

func (t *WebSearchTool) Name() string { return "web_search" }
func (t *WebSearchTool) Description() string {
	return "Search the web using Google Custom Search API and return titles, URLs, and snippets"
}
func (t *WebSearchTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"query":       {Type: "string", Description: "Search query", Required: true},
		"api_key":     {Type: "string", Description: "Google Custom Search API key", Required: true},
		"cx":          {Type: "string", Description: "Google Custom Search engine ID", Required: true},
		"num_results": {Type: "string", Description: "Number of results (1-10, default 5)", Required: false},
	}
}

func (t *WebSearchTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	query := input["query"]
	apiKey := input["api_key"]
	cx := input["cx"]
	if query == "" || apiKey == "" || cx == "" {
		return "", fmt.Errorf("query, api_key, and cx are required")
	}

	numResults := 5
	if n := input["num_results"]; n != "" {
		if parsed, err := strconv.Atoi(n); err == nil && parsed >= 1 && parsed <= 10 {
			numResults = parsed
		}
	}

	u, _ := url.Parse(t.apiURL)
	q := u.Query()
	q.Set("key", apiKey)
	q.Set("cx", cx)
	q.Set("q", query)
	q.Set("num", strconv.Itoa(numResults))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("search API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Items []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	if len(result.Items) == 0 {
		return "No results found.", nil
	}

	var output string
	for i, item := range result.Items {
		output += fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, item.Title, item.Link, item.Snippet)
	}
	return output, nil
}
