package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// GitHubSearcher searches GitHub for Cyntr skill repositories.
type GitHubSearcher struct {
	client *http.Client
	apiURL string
}

// NewGitHubSearcher creates a GitHub skill searcher.
func NewGitHubSearcher() *GitHubSearcher {
	return &GitHubSearcher{
		client: &http.Client{Timeout: 10 * time.Second},
		apiURL: "https://api.github.com",
	}
}

// SetAPIURL overrides the GitHub API URL (for testing).
func (g *GitHubSearcher) SetAPIURL(url string) { g.apiURL = url }

type githubSearchResponse struct {
	Items []struct {
		Name        string `json:"name"`
		FullName    string `json:"full_name"`
		Description string `json:"description"`
		HTMLURL     string `json:"html_url"`
		Owner       struct {
			Login string `json:"login"`
		} `json:"owner"`
		DefaultBranch string `json:"default_branch"`
	} `json:"items"`
}

// Search finds Cyntr skill repositories on GitHub.
func (g *GitHubSearcher) Search(ctx context.Context, query string) ([]MarketplaceEntry, error) {
	// Search for repos with topic "cyntr-skill" and matching query
	searchQuery := fmt.Sprintf("%s topic:cyntr-skill", query)
	u := fmt.Sprintf("%s/search/repositories?q=%s&sort=stars&per_page=20", g.apiURL, url.QueryEscape(searchQuery))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "Cyntr/0.6.0")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github API error %d: %s", resp.StatusCode, string(body))
	}

	var result githubSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var entries []MarketplaceEntry
	for _, item := range result.Items {
		branch := item.DefaultBranch
		if branch == "" {
			branch = "main"
		}
		entries = append(entries, MarketplaceEntry{
			Name:        item.Name,
			Author:      item.Owner.Login,
			Description: item.Description,
			DownloadURL: fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/skill.yaml", item.FullName, branch),
		})
	}

	return entries, nil
}
