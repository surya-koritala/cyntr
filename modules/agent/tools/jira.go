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

type JiraTool struct {
	client *http.Client
}

func NewJiraTool() *JiraTool {
	return &JiraTool{client: &http.Client{Timeout: 30 * time.Second}}
}

func (t *JiraTool) Name() string        { return "jira" }
func (t *JiraTool) Description() string { return "Interact with Jira: search issues, create issues, add comments, transition status" }
func (t *JiraTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"action":   {Type: "string", Description: "Action: search, create_issue, add_comment, get_issue", Required: true},
		"base_url": {Type: "string", Description: "Jira instance URL (e.g., https://company.atlassian.net)", Required: true},
		"email":    {Type: "string", Description: "Jira email for auth", Required: true},
		"token":    {Type: "string", Description: "Jira API token", Required: true},
		"project":  {Type: "string", Description: "Project key (for create_issue)", Required: false},
		"query":    {Type: "string", Description: "JQL query (for search)", Required: false},
		"key":      {Type: "string", Description: "Issue key like PROJ-123 (for get_issue, add_comment)", Required: false},
		"title":    {Type: "string", Description: "Summary (for create_issue)", Required: false},
		"body":     {Type: "string", Description: "Description/comment body", Required: false},
	}
}

func (t *JiraTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	action := input["action"]
	baseURL := input["base_url"]
	email := input["email"]
	token := input["token"]

	if action == "" || baseURL == "" || email == "" || token == "" {
		return "", fmt.Errorf("action, base_url, email, and token are required")
	}

	switch action {
	case "search":
		return t.search(ctx, baseURL, email, token, input["query"])
	case "get_issue":
		return t.getIssue(ctx, baseURL, email, token, input["key"])
	case "create_issue":
		return t.createIssue(ctx, baseURL, email, token, input["project"], input["title"], input["body"])
	case "add_comment":
		return t.addComment(ctx, baseURL, email, token, input["key"], input["body"])
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

func (t *JiraTool) doRequest(ctx context.Context, method, url, email, token string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(email, token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("jira API %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func (t *JiraTool) search(ctx context.Context, baseURL, email, token, jql string) (string, error) {
	if jql == "" {
		return "", fmt.Errorf("query required for search")
	}
	data, err := t.doRequest(ctx, "GET", fmt.Sprintf("%s/rest/api/3/search?jql=%s&maxResults=10", baseURL, jql), email, token, nil)
	if err != nil {
		return "", err
	}

	var result struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Status  struct {
					Name string `json:"name"`
				} `json:"status"`
			} `json:"fields"`
		} `json:"issues"`
	}
	json.Unmarshal(data, &result)

	output := "Jira search results:\n\n"
	for _, issue := range result.Issues {
		output += fmt.Sprintf("%s — %s [%s]\n", issue.Key, issue.Fields.Summary, issue.Fields.Status.Name)
	}
	if len(result.Issues) == 0 {
		output += "No results."
	}
	return output, nil
}

func (t *JiraTool) getIssue(ctx context.Context, baseURL, email, token, key string) (string, error) {
	if key == "" {
		return "", fmt.Errorf("key required")
	}
	data, err := t.doRequest(ctx, "GET", fmt.Sprintf("%s/rest/api/3/issue/%s", baseURL, key), email, token, nil)
	if err != nil {
		return "", err
	}

	var issue struct {
		Key    string `json:"key"`
		Fields struct {
			Summary     string `json:"summary"`
			Description any    `json:"description"`
			Status      struct {
				Name string `json:"name"`
			} `json:"status"`
			Assignee struct {
				DisplayName string `json:"displayName"`
			} `json:"assignee"`
		} `json:"fields"`
	}
	json.Unmarshal(data, &issue)

	return fmt.Sprintf("%s: %s\nStatus: %s\nAssignee: %s", issue.Key, issue.Fields.Summary, issue.Fields.Status.Name, issue.Fields.Assignee.DisplayName), nil
}

func (t *JiraTool) createIssue(ctx context.Context, baseURL, email, token, project, title, body string) (string, error) {
	if project == "" || title == "" {
		return "", fmt.Errorf("project and title required")
	}

	payload := map[string]any{
		"fields": map[string]any{
			"project":   map[string]string{"key": project},
			"summary":   title,
			"issuetype": map[string]string{"name": "Task"},
		},
	}
	if body != "" {
		payload["fields"].(map[string]any)["description"] = map[string]any{
			"type": "doc", "version": 1,
			"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": body}}}},
		}
	}

	data, err := t.doRequest(ctx, "POST", fmt.Sprintf("%s/rest/api/3/issue", baseURL), email, token, payload)
	if err != nil {
		return "", err
	}

	var result struct {
		Key  string `json:"key"`
		Self string `json:"self"`
	}
	json.Unmarshal(data, &result)
	return fmt.Sprintf("Created %s: %s/browse/%s", result.Key, baseURL, result.Key), nil
}

func (t *JiraTool) addComment(ctx context.Context, baseURL, email, token, key, body string) (string, error) {
	if key == "" || body == "" {
		return "", fmt.Errorf("key and body required")
	}

	payload := map[string]any{
		"body": map[string]any{
			"type": "doc", "version": 1,
			"content": []map[string]any{{"type": "paragraph", "content": []map[string]any{{"type": "text", "text": body}}}},
		},
	}

	_, err := t.doRequest(ctx, "POST", fmt.Sprintf("%s/rest/api/3/issue/%s/comment", baseURL, key), email, token, payload)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Comment added to %s", key), nil
}
