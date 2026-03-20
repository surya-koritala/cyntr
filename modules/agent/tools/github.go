package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// GitHubTool provides GitHub API operations for agents.
type GitHubTool struct {
	client *http.Client
	apiURL string
}

func NewGitHubTool() *GitHubTool {
	return &GitHubTool{
		client: &http.Client{Timeout: 30 * time.Second},
		apiURL: "https://api.github.com",
	}
}

func (t *GitHubTool) SetAPIURL(url string) { t.apiURL = url }

func (t *GitHubTool) Name() string        { return "github" }
func (t *GitHubTool) Description() string { return "Interact with GitHub: list PRs, create issues, add comments" }
func (t *GitHubTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"action": {Type: "string", Description: "Action: list_prs, get_pr, create_issue, add_comment", Required: true},
		"repo":   {Type: "string", Description: "Repository (owner/name)", Required: true},
		"token":  {Type: "string", Description: "GitHub personal access token", Required: true},
		"number": {Type: "string", Description: "PR/issue number (for get_pr, add_comment)", Required: false},
		"title":  {Type: "string", Description: "Title (for create_issue)", Required: false},
		"body":   {Type: "string", Description: "Body text (for create_issue, add_comment)", Required: false},
	}
}

func (t *GitHubTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	action := input["action"]
	repo := input["repo"]
	token := input["token"]

	if action == "" || repo == "" || token == "" {
		return "", fmt.Errorf("action, repo, and token are required")
	}

	switch action {
	case "list_prs":
		return t.listPRs(ctx, repo, token)
	case "get_pr":
		return t.getPR(ctx, repo, token, input["number"])
	case "create_issue":
		return t.createIssue(ctx, repo, token, input["title"], input["body"])
	case "add_comment":
		return t.addComment(ctx, repo, token, input["number"], input["body"])
	default:
		return "", fmt.Errorf("unknown action: %s (supported: list_prs, get_pr, create_issue, add_comment)", action)
	}
}

func (t *GitHubTool) doRequest(ctx context.Context, method, url, token string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github API: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("github API %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func (t *GitHubTool) listPRs(ctx context.Context, repo, token string) (string, error) {
	data, err := t.doRequest(ctx, "GET", fmt.Sprintf("%s/repos/%s/pulls?state=open&per_page=10", t.apiURL, repo), token, nil)
	if err != nil {
		return "", err
	}

	var prs []struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		State string `json:"state"`
	}
	json.Unmarshal(data, &prs)

	result := fmt.Sprintf("Open PRs for %s:\n\n", repo)
	for _, pr := range prs {
		result += fmt.Sprintf("#%d — %s (by @%s)\n", pr.Number, pr.Title, pr.User.Login)
	}
	if len(prs) == 0 {
		result += "No open PRs."
	}
	return result, nil
}

func (t *GitHubTool) getPR(ctx context.Context, repo, token, number string) (string, error) {
	if number == "" {
		return "", fmt.Errorf("number required for get_pr")
	}
	data, err := t.doRequest(ctx, "GET", fmt.Sprintf("%s/repos/%s/pulls/%s", t.apiURL, repo, number), token, nil)
	if err != nil {
		return "", err
	}

	var pr struct {
		Number    int    `json:"number"`
		Title     string `json:"title"`
		Body      string `json:"body"`
		State     string `json:"state"`
		Diff      string `json:"diff_url"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	json.Unmarshal(data, &pr)

	return fmt.Sprintf("PR #%d: %s\nAuthor: @%s\nState: %s\n+%d/-%d lines\n\n%s", pr.Number, pr.Title, pr.User.Login, pr.State, pr.Additions, pr.Deletions, pr.Body), nil
}

func (t *GitHubTool) createIssue(ctx context.Context, repo, token, title, body string) (string, error) {
	if title == "" {
		return "", fmt.Errorf("title required for create_issue")
	}
	data, err := t.doRequest(ctx, "POST", fmt.Sprintf("%s/repos/%s/issues", t.apiURL, repo), token, map[string]string{"title": title, "body": body})
	if err != nil {
		return "", err
	}

	var issue struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	json.Unmarshal(data, &issue)

	return fmt.Sprintf("Created issue #%d: %s", issue.Number, issue.HTMLURL), nil
}

func (t *GitHubTool) addComment(ctx context.Context, repo, token, number, body string) (string, error) {
	if number == "" || body == "" {
		return "", fmt.Errorf("number and body required for add_comment")
	}
	_, err := t.doRequest(ctx, "POST", fmt.Sprintf("%s/repos/%s/issues/%s/comments", t.apiURL, repo, number), token, map[string]string{"body": body})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Comment added to #%s", number), nil
}
