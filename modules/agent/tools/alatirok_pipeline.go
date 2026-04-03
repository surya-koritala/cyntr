package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// AlatirokPipelineTool orchestrates multi-step Alatirok workflows in Go code,
// using the LLM only for content generation. This lets smaller local models
// (Qwen3.5, Nemotron, Mistral Nemo) participate because they only need to
// write text — not manage tool chains.
//
// Pipelines:
//   - write_post: Go fetches news + resolves community → LLM writes post → Go posts to Alatirok
//   - write_comment: Go fetches post + comments → LLM writes reply → Go posts comment
type AlatirokPipelineTool struct {
	news     *NewsAggregatorTool
	alatirok *AlatirokTool
}

func NewAlatirokPipelineTool(news *NewsAggregatorTool, alatirok *AlatirokTool) *AlatirokPipelineTool {
	return &AlatirokPipelineTool{news: news, alatirok: alatirok}
}

func (t *AlatirokPipelineTool) Name() string { return "alatirok_pipeline" }
func (t *AlatirokPipelineTool) Description() string {
	return "Orchestrated Alatirok workflows. Use 'write_post' to fetch news and create a post in one step, or 'write_comment' to read a post and write a discussion reply. You just provide the content — the pipeline handles the API calls."
}

func (t *AlatirokPipelineTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"pipeline": {Type: "string", Description: "Pipeline: write_post or write_comment", Required: true},
		"community_slug": {Type: "string", Description: "Community slug for write_post (e.g. science, space, biotech)", Required: false},
		"news_category":  {Type: "string", Description: "News category for write_post (e.g. science, space, ai, biotech, finance, environment)", Required: false},
		"title":          {Type: "string", Description: "Your original post title (for write_post)", Required: false},
		"body":           {Type: "string", Description: "Your post body or comment text with markdown (for write_post or write_comment)", Required: false},
		"post_id":        {Type: "string", Description: "Post ID to comment on (for write_comment)", Required: false},
		"reply_to":       {Type: "string", Description: "Comment ID to reply to for threading (for write_comment, optional)", Required: false},
	}
}

func (t *AlatirokPipelineTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	apiKey := input["api_key"]
	if apiKey == "" {
		apiKey = input["alatirok_api_key"]
	}

	pipeline := input["pipeline"]
	switch pipeline {
	case "write_post":
		return t.writePost(ctx, apiKey, input)
	case "write_comment":
		return t.writeComment(ctx, apiKey, input)
	default:
		return "", fmt.Errorf("unknown pipeline: %s (use write_post or write_comment)", pipeline)
	}
}

func (t *AlatirokPipelineTool) writePost(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	title := input["title"]
	body := input["body"]
	community := input["community_slug"]
	newsCat := input["news_category"]

	if title == "" || body == "" {
		// Fetch news to give context — return articles for the LLM to choose from
		if newsCat == "" {
			newsCat = "general"
		}
		articles, err := t.news.Execute(ctx, map[string]string{"category": newsCat, "limit": "5"})
		if err != nil {
			return "", fmt.Errorf("fetch news: %w", err)
		}
		if community == "" {
			community = newsCat
		}
		return fmt.Sprintf(`NEWS ARTICLES (pick one and write your post):

%s

Now call this tool again with:
- pipeline: write_post
- community_slug: %s
- title: YOUR original title about the article
- body: 3-5 paragraphs with ## headings, your analysis, and Source: [Title](URL) at the bottom`, articles, community), nil
	}

	// Fix newlines
	body = strings.ReplaceAll(body, "\\n", "\n")
	body = strings.TrimRight(body, "\",\n ")

	// Resolve community slug to ID
	commResp, err := t.alatirok.doGet(ctx, "/api/v1/communities/"+community, apiKey)
	communityID := ""
	if err == nil {
		var commData struct {
			Data struct{ ID string `json:"id"` } `json:"data"`
			ID   string `json:"id"`
		}
		if json.Unmarshal([]byte(commResp), &commData) == nil {
			communityID = commData.Data.ID
			if communityID == "" {
				communityID = commData.ID
			}
		}
	}

	// Post directly
	payload := map[string]any{
		"title":     title,
		"body":      body,
		"post_type": "text",
	}
	if communityID != "" {
		payload["community_id"] = communityID
	}

	result, err := t.alatirok.doPost(ctx, "/api/v1/posts", apiKey, payload)
	if err != nil {
		return "", fmt.Errorf("create post: %w", err)
	}

	return "POST CREATED SUCCESSFULLY:\n" + result, nil
}

func (t *AlatirokPipelineTool) writeComment(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	postID := input["post_id"]
	body := input["body"]
	replyTo := input["reply_to"]

	if postID == "" {
		// No post specified — get a random recent post for discussion
		feedResult, err := t.alatirok.getFeed(ctx, apiKey, map[string]string{"sort": "new", "limit": "10"})
		if err != nil {
			return "", fmt.Errorf("get feed: %w", err)
		}

		// Parse feed and pick a post with few comments
		var feedData struct {
			Data []struct {
				ID           string `json:"id"`
				Title        string `json:"title"`
				Body         string `json:"body"`
				CommentCount int    `json:"comment_count"`
			} `json:"data"`
		}
		if json.Unmarshal([]byte(feedResult), &feedData) == nil && len(feedData.Data) > 0 {
			// Pick a post with < 5 comments
			var candidates []int
			for i, p := range feedData.Data {
				if p.CommentCount < 5 {
					candidates = append(candidates, i)
				}
			}
			if len(candidates) == 0 {
				candidates = []int{0}
			}
			pick := feedData.Data[candidates[rand.Intn(len(candidates))]]
			postID = pick.ID

			return fmt.Sprintf(`POST TO DISCUSS:
Title: %s
Body: %s

EXISTING COMMENTS: (check with pipeline=write_comment, post_id=%s after reading)

Write your reply and call this tool again with:
- pipeline: write_comment
- post_id: %s
- body: Your 2-paragraph comment referencing specific content from the post
- reply_to: (optional) a comment ID if you want to reply to someone`, pick.Title, pick.Body[:min(800, len(pick.Body))], pick.ID, pick.ID), nil
		}
		return feedResult, nil
	}

	if body == "" {
		// Fetch post + comments for the LLM to read
		postResult, _ := t.alatirok.getPost(ctx, apiKey, postID)
		commentsResult, _ := t.alatirok.getComments(ctx, apiKey, postID)

		return fmt.Sprintf(`POST CONTENT:
%s

EXISTING COMMENTS:
%s

Write your reply and call this tool again with:
- pipeline: write_comment
- post_id: %s
- body: Your 2-paragraph reply (reference specific content, stay on topic)
- reply_to: (optional) comment ID to create a threaded reply`, postResult, commentsResult, postID), nil
	}

	// Post the comment directly
	body = strings.ReplaceAll(body, "\\n", "\n")
	payload := map[string]any{"body": body}
	if replyTo != "" {
		payload["parent_comment_id"] = replyTo
	}

	result, err := t.alatirok.doPost(ctx, "/api/v1/posts/"+postID+"/comments", apiKey, payload)
	if err != nil {
		return "", fmt.Errorf("create comment: %w", err)
	}

	// Also upvote the post
	t.alatirok.doPost(ctx, "/api/v1/votes", apiKey, map[string]any{
		"target_id": postID, "target_type": "post", "direction": "up",
	})

	return "COMMENT POSTED:\n" + result, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
