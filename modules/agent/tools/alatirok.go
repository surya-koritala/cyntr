package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// AlatirokTool provides integration with the Alatirok social platform for AI agents.
// Supports posting, commenting, voting, searching, and browsing communities.
type AlatirokTool struct {
	client  *http.Client
	baseURL string
}

func NewAlatirokTool() *AlatirokTool {
	baseURL := os.Getenv("ALATIROK_BASE_URL")
	if baseURL == "" {
		baseURL = "https://www.alatirok.com"
	}
	return &AlatirokTool{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: strings.TrimRight(baseURL, "/"),
	}
}

func (t *AlatirokTool) Name() string { return "alatirok" }
func (t *AlatirokTool) Description() string {
	return "Interact with Alatirok — a social platform for AI agents. Post content, comment, vote, search, and browse communities."
}

func (t *AlatirokTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"action": {Type: "string", Description: "Action: whoami, get_feed, create_post, edit_post, get_post, get_comments, create_comment, vote, react, search, get_communities, get_community_feed, subscribe, bookmark, my_posts, my_comments, notifications, trust_info", Required: true},
		"reaction_type": {Type: "string", Description: "Reaction type: like, insightful, disagree (for react action)", Required: false},
		"title":  {Type: "string", Description: "Post title (for create_post)", Required: false},
		"body":   {Type: "string", Description: "Markdown body (for create_post, create_comment)", Required: false},
		"community_id":   {Type: "string", Description: "Community UUID (for create_post)", Required: false},
		"community_slug": {Type: "string", Description: "Community slug (for get_community_feed)", Required: false},
		"post_type": {Type: "string", Description: "Post type: text, link, research, alert, meta, question, data (default: text)", Required: false},
		"tags":       {Type: "string", Description: "Comma-separated tags (for create_post)", Required: false},
		"post_id":    {Type: "string", Description: "Post UUID (for get_post, create_comment, vote)", Required: false},
		"parent_comment_id": {Type: "string", Description: "Parent comment UUID for replies (for create_comment)", Required: false},
		"target_id":   {Type: "string", Description: "Target UUID (for vote)", Required: false},
		"target_type": {Type: "string", Description: "Target type: post or comment (for vote)", Required: false},
		"direction":   {Type: "string", Description: "Vote direction: up or down (for vote)", Required: false},
		"query":       {Type: "string", Description: "Search query (for search)", Required: false},
		"sort":        {Type: "string", Description: "Sort: hot, new, top (default: hot)", Required: false},
		"limit":       {Type: "string", Description: "Number of results (default: 25)", Required: false},
		"metadata":    {Type: "string", Description: "JSON metadata object (for create_post, e.g. {\"confidence\": 0.9})", Required: false},
		"image_url":   {Type: "string", Description: "Image URL to include in post body (for create_post)", Required: false},
	}
}

func (t *AlatirokTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	// Check input first (per-agent key via secrets injection), then env var
	apiKey := input["api_key"]
	if apiKey == "" {
		apiKey = input["alatirok_api_key"]
	}
	if apiKey == "" {
		apiKey = os.Getenv("ALATIROK_API_KEY")
	}
	if apiKey == "" {
		return "", fmt.Errorf("ALATIROK_API_KEY not set — register an agent at https://alatirok.com and create an API key")
	}

	// Send heartbeat on every authenticated action (marks agent as "online")
	go t.heartbeat(context.Background(), apiKey)

	action := input["action"]
	switch action {
	case "whoami":
		return t.whoami(ctx, apiKey)
	case "get_feed":
		return t.getFeed(ctx, apiKey, input)
	case "create_post":
		return t.createPost(ctx, apiKey, input)
	case "get_post":
		return t.getPost(ctx, apiKey, input["post_id"])
	case "edit_post":
		return t.editPost(ctx, apiKey, input)
	case "get_comments":
		return t.getComments(ctx, apiKey, input["post_id"])
	case "create_comment":
		return t.createComment(ctx, apiKey, input)
	case "vote":
		return t.vote(ctx, apiKey, input)
	case "react":
		return t.react(ctx, apiKey, input)
	case "search":
		return t.search(ctx, apiKey, input)
	case "get_communities":
		return t.getCommunities(ctx, apiKey)
	case "get_community_feed":
		return t.getCommunityFeed(ctx, apiKey, input)
	case "my_posts":
		return t.myPosts(ctx, apiKey)
	case "my_comments":
		return t.myComments(ctx, apiKey)
	case "notifications":
		return t.notifications(ctx, apiKey)
	case "subscribe":
		return t.subscribe(ctx, apiKey, input)
	case "bookmark":
		return t.bookmark(ctx, apiKey, input["post_id"])
	case "trust_info":
		return t.trustInfo(ctx, apiKey)
	case "poll_create":
		return t.pollCreate(ctx, apiKey, input)
	case "poll_vote":
		return t.pollVote(ctx, apiKey, input)
	case "poll_get":
		return t.pollGet(ctx, apiKey, input["post_id"])
	case "activity":
		return t.activity(ctx, apiKey)
	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}

func (t *AlatirokTool) whoami(ctx context.Context, apiKey string) (string, error) {
	return t.doGet(ctx, "/api/v1/auth/me", apiKey)
}

func (t *AlatirokTool) getFeed(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	sort := input["sort"]
	if sort == "" {
		sort = "hot"
	}
	limit := input["limit"]
	if limit == "" {
		limit = "25"
	}
	path := fmt.Sprintf("/api/v1/feed?sort=%s&limit=%s", sort, limit)
	if pt := input["post_type"]; pt != "" {
		path += "&type=" + pt
	}
	return t.doGet(ctx, path, apiKey)
}

func (t *AlatirokTool) createPost(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	if input["title"] == "" {
		return "", fmt.Errorf("title is required for create_post")
	}
	if input["body"] == "" {
		return "", fmt.Errorf("body is required for create_post")
	}

	postType := input["post_type"]
	if postType == "" {
		postType = "text"
	}

	body := input["body"]
	if input["image_url"] != "" {
		body += "\n\n![image](" + input["image_url"] + ")"
	}

	payload := map[string]any{
		"title":     input["title"],
		"body":      body,
		"post_type": postType,
	}

	if input["community_id"] != "" {
		payload["community_id"] = input["community_id"]
	}

	if input["tags"] != "" {
		tags := strings.Split(input["tags"], ",")
		for i := range tags {
			tags[i] = strings.TrimSpace(tags[i])
		}
		payload["tags"] = tags
	}

	if input["metadata"] != "" {
		var meta map[string]any
		if json.Unmarshal([]byte(input["metadata"]), &meta) == nil {
			payload["metadata"] = meta
		}
	}

	return t.doPost(ctx, "/api/v1/posts", apiKey, payload)
}

func (t *AlatirokTool) getPost(ctx context.Context, apiKey string, postID string) (string, error) {
	if postID == "" {
		return "", fmt.Errorf("post_id is required for get_post")
	}
	return t.doGet(ctx, "/api/v1/posts/"+postID, apiKey)
}

func (t *AlatirokTool) createComment(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	postID := input["post_id"]
	if postID == "" {
		return "", fmt.Errorf("post_id is required for create_comment")
	}
	if input["body"] == "" {
		return "", fmt.Errorf("body is required for create_comment")
	}

	payload := map[string]any{
		"body": input["body"],
	}
	if input["parent_comment_id"] != "" {
		payload["parent_comment_id"] = input["parent_comment_id"]
	}

	return t.doPost(ctx, "/api/v1/posts/"+postID+"/comments", apiKey, payload)
}

func (t *AlatirokTool) vote(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	targetID := input["target_id"]
	if targetID == "" {
		targetID = input["post_id"]
	}
	if targetID == "" {
		return "", fmt.Errorf("target_id or post_id is required for vote")
	}

	targetType := input["target_type"]
	if targetType == "" {
		targetType = "post"
	}

	direction := input["direction"]
	if direction == "" {
		direction = "up"
	}

	payload := map[string]any{
		"target_id":   targetID,
		"target_type": targetType,
		"direction":   direction,
	}

	return t.doPost(ctx, "/api/v1/votes", apiKey, payload)
}

func (t *AlatirokTool) search(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	query := input["query"]
	if query == "" {
		return "", fmt.Errorf("query is required for search")
	}
	limit := input["limit"]
	if limit == "" {
		limit = "25"
	}
	path := fmt.Sprintf("/api/v1/search?q=%s&limit=%s", url.QueryEscape(query), limit)
	return t.doGet(ctx, path, apiKey)
}

func (t *AlatirokTool) getCommunities(ctx context.Context, apiKey string) (string, error) {
	return t.doGet(ctx, "/api/v1/communities", apiKey)
}

func (t *AlatirokTool) getCommunityFeed(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	slug := input["community_slug"]
	if slug == "" {
		return "", fmt.Errorf("community_slug is required for get_community_feed")
	}
	sort := input["sort"]
	if sort == "" {
		sort = "hot"
	}
	limit := input["limit"]
	if limit == "" {
		limit = "25"
	}
	path := fmt.Sprintf("/api/v1/communities/%s/feed?sort=%s&limit=%s", slug, sort, limit)
	return t.doGet(ctx, path, apiKey)
}

// --- HTTP helpers ---

func (t *AlatirokTool) editPost(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	postID := input["post_id"]
	if postID == "" {
		return "", fmt.Errorf("post_id is required for edit_post")
	}
	payload := map[string]any{}
	if input["title"] != "" {
		payload["title"] = input["title"]
	}
	if input["body"] != "" {
		payload["body"] = input["body"]
	}
	return t.doPatch(ctx, "/api/v1/posts/"+postID, apiKey, payload)
}

func (t *AlatirokTool) getComments(ctx context.Context, apiKey string, postID string) (string, error) {
	if postID == "" {
		return "", fmt.Errorf("post_id is required for get_comments")
	}
	return t.doGet(ctx, "/api/v1/posts/"+postID+"/comments", apiKey)
}

func (t *AlatirokTool) react(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	commentID := input["target_id"]
	if commentID == "" {
		return "", fmt.Errorf("target_id (comment ID) is required for react")
	}
	reactionType := input["reaction_type"]
	if reactionType == "" {
		reactionType = "like"
	}
	return t.doPost(ctx, "/api/v1/comments/"+commentID+"/reactions", apiKey, map[string]string{"type": reactionType})
}

func (t *AlatirokTool) myPosts(ctx context.Context, apiKey string) (string, error) {
	return t.doGet(ctx, "/api/v1/me/posts", apiKey)
}

func (t *AlatirokTool) myComments(ctx context.Context, apiKey string) (string, error) {
	return t.doGet(ctx, "/api/v1/me/comments", apiKey)
}

func (t *AlatirokTool) notifications(ctx context.Context, apiKey string) (string, error) {
	return t.doGet(ctx, "/api/v1/notifications", apiKey)
}

func (t *AlatirokTool) subscribe(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	slug := input["community_slug"]
	if slug == "" {
		return "", fmt.Errorf("community_slug is required for subscribe")
	}
	return t.doPost(ctx, "/api/v1/communities/"+slug+"/subscribe", apiKey, nil)
}

func (t *AlatirokTool) bookmark(ctx context.Context, apiKey string, postID string) (string, error) {
	if postID == "" {
		return "", fmt.Errorf("post_id is required for bookmark")
	}
	return t.doPost(ctx, "/api/v1/posts/"+postID+"/bookmark", apiKey, nil)
}

func (t *AlatirokTool) trustInfo(ctx context.Context, apiKey string) (string, error) {
	return t.doGet(ctx, "/api/v1/trust-info", apiKey)
}

func (t *AlatirokTool) pollCreate(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	postID := input["post_id"]
	if postID == "" {
		return "", fmt.Errorf("post_id is required for poll_create")
	}
	options := strings.Split(input["options"], ",")
	for i := range options {
		options[i] = strings.TrimSpace(options[i])
	}
	payload := map[string]any{"options": options}
	if input["deadline"] != "" {
		payload["deadline"] = input["deadline"]
	}
	return t.doPost(ctx, "/api/v1/posts/"+postID+"/poll", apiKey, payload)
}

func (t *AlatirokTool) pollVote(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	postID := input["post_id"]
	if postID == "" {
		return "", fmt.Errorf("post_id is required for poll_vote")
	}
	return t.doPost(ctx, "/api/v1/posts/"+postID+"/poll/vote", apiKey, map[string]string{
		"option_id": input["option_id"],
	})
}

func (t *AlatirokTool) pollGet(ctx context.Context, apiKey string, postID string) (string, error) {
	if postID == "" {
		return "", fmt.Errorf("post_id is required for poll_get")
	}
	return t.doGet(ctx, "/api/v1/posts/"+postID+"/poll", apiKey)
}

func (t *AlatirokTool) activity(ctx context.Context, apiKey string) (string, error) {
	return t.doGet(ctx, "/api/v1/activity/recent?limit=20", apiKey)
}

func (t *AlatirokTool) heartbeat(ctx context.Context, apiKey string) {
	req, err := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/api/v1/heartbeat", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := t.client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// --- HTTP helpers ---

func (t *AlatirokTool) doPatch(ctx context.Context, path, apiKey string, payload any) (string, error) {
	jsonBody, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "PATCH", t.baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Alatirok API error (HTTP %d): %s", resp.StatusCode, string(body))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		return pretty.String(), nil
	}
	return string(body), nil
}

func (t *AlatirokTool) doGet(ctx context.Context, path, apiKey string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", t.baseURL+path, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Alatirok API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	// Pretty-print JSON for readability
	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		return pretty.String(), nil
	}
	return string(body), nil
}

func (t *AlatirokTool) doPost(ctx context.Context, path, apiKey string, payload any) (string, error) {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", t.baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Alatirok API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var pretty bytes.Buffer
	if json.Indent(&pretty, body, "", "  ") == nil {
		return pretty.String(), nil
	}
	return string(body), nil
}
