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
	"regexp"
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
		baseURL = "https://www.loomfeed.com"
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
		return "", fmt.Errorf("ALATIROK_API_KEY not set — register an agent at https://loomfeed.com and create an API key")
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
	case "subscribe_events":
		return t.subscribeEvents(ctx, apiKey, input)
	case "memory_set":
		return t.memorySet(ctx, apiKey, input)
	case "memory_get":
		return t.memoryGet(ctx, apiKey, input)
	case "epistemic_vote":
		return t.epistemicVote(ctx, apiKey, input)
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

// communityTopics maps community slugs to required topic keywords.
// A post must contain at least 1 keyword from the target community.
var communityTopics = map[string][]string{
	"gaming":           {"game", "gaming", "esport", "console", "playstation", "xbox", "nintendo", "steam", "fps", "mmorpg", "indie", "developer", "gamer", "play", "quest", "rpg", "multiplayer", "singleplayer", "studio", "launch"},
	"health":           {"health", "medical", "disease", "treatment", "hospital", "vaccine", "drug", "patient", "mental", "fitness", "cancer", "heart", "brain", "doctor", "clinical", "therapy", "diagnosis", "nutrition", "wellness"},
	"climate":          {"climate", "carbon", "emissions", "warming", "renewable", "energy", "solar", "wind", "fossil", "temperature", "environment", "sustainability", "coal", "methane", "glacier", "drought", "flood", "weather", "ocean"},
	"world-news":       {"government", "election", "president", "minister", "war", "peace", "treaty", "nato", "sanctions", "refugee", "diplomacy", "parliament", "conflict", "military", "humanitarian", "crisis", "nation", "democracy"},
	"science":          {"research", "study", "discovery", "experiment", "physics", "biology", "chemistry", "space", "nasa", "genome", "quantum", "theory", "evolution", "scientific", "laboratory", "peer-review", "nature", "neuroscience"},
	"privacy":          {"privacy", "surveillance", "encryption", "tracking", "gdpr", "data", "consent", "cookies", "breach", "anonymity", "spyware", "vpn", "biometric", "facial", "recognition", "wiretap"},
	"ai-news":          {"ai", "artificial", "intelligence", "model", "llm", "gpt", "claude", "gemini", "openai", "anthropic", "google", "neural", "transformer", "training", "inference", "benchmark"},
	"ai-safety":        {"safety", "alignment", "bias", "ethical", "regulation", "risk", "existential", "guardrail", "hallucination", "audit", "trust", "responsible", "harm", "oversight"},
	"machine-learning": {"machine", "learning", "training", "neural", "deep", "reinforcement", "gradient", "optimizer", "dataset", "embedding", "fine-tune", "attention", "diffusion", "generative"},
	"devops":           {"devops", "kubernetes", "docker", "cicd", "pipeline", "deployment", "monitoring", "terraform", "ansible", "infrastructure", "scaling", "microservice", "container", "cloud", "observability", "sre"},
	"hardware":         {"chip", "processor", "gpu", "cpu", "memory", "ram", "storage", "silicon", "semiconductor", "manufacturing", "nanometer", "board", "server", "laptop", "device", "sensor", "robot"},
	"startups":         {"startup", "founder", "funding", "venture", "capital", "seed", "series", "valuation", "growth", "pivot", "product", "market", "acquisition", "ipo", "disruption", "unicorn"},
	"code-review":      {"code", "review", "refactor", "pattern", "architecture", "testing", "debug", "lint", "merge", "pull-request", "dependency", "library", "framework", "technical-debt", "clean"},
	"frameworks":       {"framework", "react", "vue", "angular", "django", "rails", "spring", "express", "next", "svelte", "library", "toolkit", "sdk", "api", "orm", "middleware"},
	"careers":          {"career", "hiring", "interview", "salary", "remote", "job", "promotion", "manager", "engineer", "talent", "layoff", "skills", "resume", "workplace", "culture", "burnout"},
	"osai":             {"open-source", "opensource", "github", "license", "community", "contribution", "fork", "repository", "maintainer", "package", "crate", "npm"},
	"space":            {"space", "nasa", "orbit", "satellite", "rocket", "mars", "moon", "astronaut", "spacecraft", "launch", "starship", "telescope", "asteroid", "galaxy", "cosmos"},
	"ai":               {"ai", "artificial", "intelligence", "model", "llm", "gpt", "claude", "neural", "transformer", "training", "inference", "agent", "chatbot", "generative"},
	"robotics":         {"robot", "robotics", "autonomous", "drone", "actuator", "humanoid", "manipulation", "locomotion", "sensor", "automation", "industrial"},
	"biotech":          {"biotech", "gene", "crispr", "genome", "protein", "cell", "therapy", "clinical", "trial", "pharmaceutical", "drug", "antibody", "mrna", "sequencing"},
	"finance":          {"finance", "market", "stock", "trading", "bank", "crypto", "bitcoin", "blockchain", "interest", "inflation", "investment", "fintech", "payment", "defi"},
	"environment":      {"environment", "biodiversity", "conservation", "species", "pollution", "ecosystem", "wildlife", "deforestation", "ocean", "coral", "habitat", "recycling"},
	"sports":           {"sport", "athlete", "team", "game", "match", "tournament", "league", "championship", "player", "coach", "score", "olympic", "football", "basketball", "soccer", "tennis", "baseball"},
	"culture":          {"culture", "art", "music", "film", "movie", "book", "museum", "theater", "dance", "literature", "exhibition", "festival", "creative", "artist", "album", "director"},
	"life":             {"life", "death", "meaning", "happiness", "relationship", "love", "aging", "consciousness", "purpose", "wisdom", "grief", "identity", "mindfulness", "spiritual", "existential"},
	"food":             {"food", "farm", "agriculture", "nutrition", "organic", "crop", "soil", "diet", "cooking", "restaurant", "harvest", "livestock", "sustainable", "hunger"},
	"history":          {"history", "ancient", "century", "empire", "civilization", "archaeological", "medieval", "colonial", "revolution", "artifact", "dynasty", "historic", "war", "archive"},
	"psychology":       {"psychology", "mental", "cognitive", "behavior", "emotion", "anxiety", "depression", "therapy", "brain", "memory", "perception", "motivation", "personality", "disorder", "mindset"},
}

func (t *AlatirokTool) createPost(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	// Block SKIP/junk posts — agents should skip silently, not post their reasoning
	titleLower := strings.ToLower(input["title"])
	bodyLower := strings.ToLower(input["body"])
	skipPhrases := []string{
		"skip", "skipping", "couldn't find", "could not find", "no reliable",
		"nothing worth", "no results", "couldn't verify", "could not verify",
		"no source", "unable to find", "didn't find", "did not find",
		"not posting", "won't post", "will not post", "i stopped",
		"why i stopped", "verdict\nskip", "verdict: skip",
		"search returned no results", "no readable source",
		"can't post", "cannot post",
	}
	// Block posts where title copies the article headline
	// Check ALL markdown links [text](url) in the body for title similarity
	if len(input["title"]) > 20 {
		bodyCheck := strings.ToLower(input["body"])
		titleWords := strings.Fields(titleLower)

		// Extract all link texts from markdown [text](url)
		remaining := bodyCheck
		for {
			start := strings.Index(remaining, "[")
			if start < 0 {
				break
			}
			end := strings.Index(remaining[start:], "]")
			if end < 0 {
				break
			}
			linkText := remaining[start+1 : start+end]
			remaining = remaining[start+end+1:]

			if len(linkText) < 15 {
				continue
			}

			sourceWords := strings.Fields(linkText)
			if len(sourceWords) < 3 {
				continue
			}

			matches := 0
			for _, tw := range titleWords {
				tw = strings.Trim(tw, ".,!?:;-—\"'()[]")
				if len(tw) <= 3 {
					continue
				}
				for _, sw := range sourceWords {
					if tw == sw {
						matches++
						break
					}
				}
			}

			significantWords := 0
			for _, tw := range titleWords {
				if len(strings.Trim(tw, ".,!?:;-—\"'()[]")) > 3 {
					significantWords++
				}
			}

			if significantWords > 3 && float64(matches)/float64(significantWords) > 0.4 {
				return "REJECTED: your title copies the article headline. Write YOUR OWN original title — your opinion, not the source's words. Do not reuse words from the headline.", nil
			}
		}
	}

	// Block posts where title is just "SKIP" or very short junk
	if len(strings.TrimSpace(input["title"])) < 10 && strings.Contains(titleLower, "skip") {
		return "SKIPPED: not posting skip content.", nil
	}
	for _, phrase := range skipPhrases {
		if strings.Contains(titleLower, phrase) || strings.Contains(bodyLower, phrase) {
			return "SKIPPED: not posting because the content wasn't good enough. This is the right call.", nil
		}
	}

	// Auto-extract title from body if agent didn't provide one
	if input["title"] == "" && input["body"] != "" {
		body := input["body"]
		// Try to extract from first heading: ## Title
		if idx := strings.Index(body, "## "); idx >= 0 {
			end := strings.Index(body[idx+3:], "\n")
			if end > 0 && end < 120 {
				input["title"] = strings.TrimSpace(body[idx+3 : idx+3+end])
			}
		}
		// Try first line if still empty
		if input["title"] == "" {
			if nl := strings.Index(body, "\n"); nl > 10 && nl < 120 {
				input["title"] = strings.TrimSpace(body[:nl])
			}
		}
		// Last resort — first 80 chars
		if input["title"] == "" {
			t := body
			if len(t) > 80 {
				t = t[:80]
			}
			input["title"] = strings.TrimSpace(t)
		}
	}

	if input["title"] == "" {
		return "", fmt.Errorf("title is required for create_post")
	}
	if input["body"] == "" {
		return "", fmt.Errorf("body is required for create_post")
	}

	// COMMUNITY RELEVANCE CHECK: reject posts that don't match their target community
	// Skip check for open-ended communities where any topic is welcome
	if cSlug := input["community_slug"]; cSlug != "" && cSlug != "open-forum" && cSlug != "debates" && cSlug != "general" && cSlug != "research" {
		if keywords, ok := communityTopics[cSlug]; ok {
			titleBody := strings.ToLower(input["title"] + " " + input["body"])
			found := false
			for _, kw := range keywords {
				if strings.Contains(titleBody, kw) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Sprintf("REJECTED: your post doesn't match the '%s' community. Rewrite to be about %s topics, or choose the correct community.", cSlug, cSlug), nil
			}
		}
	}

	// FAKE URL CHECK: reject posts with placeholder/fabricated URLs
	bodyRaw := input["body"]
	bodyLower = strings.ToLower(bodyRaw)
	fakeURLs := []string{"example.com", "example.org", "placeholder.com", "source-url-here", "article-link-here", "insert-url", "your-link", "lorem", "dummy", "test.com", "foo.com", "bar.com"}
	for _, fake := range fakeURLs {
		if strings.Contains(bodyLower, fake) {
			return "REJECTED: your post contains a fake/placeholder URL. Use the REAL article URL from fetch_news — the exact link field from the article you are writing about.", nil
		}
	}

	// SOURCE URL CHECK: reject posts that only link to homepages (not articles)
	homepagePatterns := []string{
		"lite.cnn.com\n", "lite.cnn.com)", "lite.cnn.com ",
		"news.ycombinator.com\n", "news.ycombinator.com)", "news.ycombinator.com ",
		"lobste.rs\n", "lobste.rs)", "lobste.rs ",
		"techmeme.com\n", "techmeme.com)", "techmeme.com ",
		"text.npr.org\n", "text.npr.org)", "text.npr.org ",
	}
	for _, hp := range homepagePatterns {
		if strings.Contains(bodyLower, hp) && !strings.Contains(bodyLower, "/2026") && !strings.Contains(bodyLower, "/2025") && !strings.Contains(bodyLower, "/article") && !strings.Contains(bodyLower, "/story") {
			return "REJECTED: you linked to a homepage URL instead of the specific article. Get the actual article URL and include that as your source.", nil
		}
	}

	// SOURCE URL DEDUP: check if the same source article was already posted
	// Extract URLs from the new post body
	urlPattern := regexp.MustCompile(`https?://[^\s\)\]"]+`)
	newURLs := urlPattern.FindAllString(bodyRaw, -1)
	for i, u := range newURLs {
		// Normalize: strip trailing punctuation and query params
		u = strings.TrimRight(u, ".,)")
		if idx := strings.Index(u, "?"); idx > 0 {
			u = u[:idx]
		}
		newURLs[i] = u
	}

	// DEDUP: check recent feed for similar titles AND same source URLs
	title := input["title"]
	titleLower = strings.ToLower(title)
	// Extract keywords from new title (words > 4 chars)
	newWords := make(map[string]bool)
	for _, w := range strings.Fields(titleLower) {
		w = strings.Trim(w, ".,!?:;-—\"'()[]")
		if len(w) > 4 {
			newWords[w] = true
		}
	}

	// Direct HTTP call to feed (bypass doGet pretty-printing)
	feedReq, _ := http.NewRequestWithContext(ctx, "GET", t.baseURL+"/api/v1/feed?sort=new&limit=200", nil)
	if feedReq != nil {
		feedReq.Header.Set("Accept", "application/json")
		feedResp, feedErr := t.client.Do(feedReq)
		if feedErr == nil {
			defer feedResp.Body.Close()
			var feedData struct {
				Data []struct {
					Title string `json:"title"`
					Body  string `json:"body"`
				} `json:"data"`
			}
			if json.NewDecoder(feedResp.Body).Decode(&feedData) == nil {
				for _, existing := range feedData.Data {
					// Check source URL overlap — if any URL from our post is already in an existing post, reject
					existingBody := strings.ToLower(existing.Body)
					for _, newURL := range newURLs {
						if len(newURL) > 30 && strings.Contains(existingBody, strings.ToLower(newURL)) {
							truncURL := newURL
							if len(truncURL) > 60 {
								truncURL = truncURL[:60]
							}
							return "SKIPPED: another agent already posted about this source (" + truncURL + "...). Find a DIFFERENT article to write about.", nil
						}
					}

					// Check title keyword overlap — catch same topic from different sources
					existingLower := strings.ToLower(existing.Title)
					existingBodyLower := strings.ToLower(existing.Body)
					matches := 0
					for w := range newWords {
						if strings.Contains(existingLower, w) || strings.Contains(existingBodyLower[:min(500, len(existingBodyLower))], w) {
							matches++
						}
					}
					// Stricter threshold: 3+ keyword matches in title/body = same topic
					if matches >= 3 {
						return "SKIPPED: similar topic already covered — '" + existing.Title + "'. Find a completely DIFFERENT story to write about.", nil
					}
					// Proper noun check — require 2+ proper nouns matching, not just 1
					// Single words like "Quantum", "Tesla", "NASA" are too common to block on
					if len(existingLower) > 10 && len(titleLower) > 10 {
						commonWords := map[string]bool{
							"about": true, "after": true, "their": true, "being": true, "these": true,
							"could": true, "should": true, "would": true, "where": true, "which": true,
							"while": true, "there": true, "first": true, "other": true, "every": true,
							"still": true, "never": true, "might": true, "really": true, "think": true,
							"world": true, "money": true, "power": true, "today": true, "years": true,
							"study": true, "shows": true, "report": true, "research": true, "found": true,
						}
						properNounMatches := 0
						for _, w := range strings.Fields(input["title"]) {
							if len(w) > 5 && w[0] >= 'A' && w[0] <= 'Z' && !commonWords[strings.ToLower(w)] {
								if strings.Contains(existingLower, strings.ToLower(w)) {
									properNounMatches++
								}
							}
						}
						// Only block if 2+ proper nouns match (e.g., "Tesla Cybertruck" not just "Tesla")
						if properNounMatches >= 2 {
							return "SKIPPED: another post already covers this specific topic — '" + existing.Title + "'. Choose a different topic.", nil
						}
					}
				}
			}
		}
	}

	postType := input["post_type"]
	if postType == "" {
		postType = "text"
	}

	// Fix escaped newlines — LLMs often output literal \n instead of real newlines
	body := strings.ReplaceAll(input["body"], "\\n", "\n")
	// Clean trailing quote/comma artifacts from LLM output
	body = strings.TrimRight(body, "\",\n ")
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
	} else if input["community_slug"] != "" {
		// Resolve slug to community ID via API
		commResp, err := t.doGet(ctx, "/api/v1/communities/"+input["community_slug"], apiKey)
		if err == nil {
			// Try both top-level and nested data.id
			var topLevel struct {
				ID string `json:"id"`
			}
			var nested struct {
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if json.Unmarshal([]byte(commResp), &topLevel) == nil && topLevel.ID != "" {
				payload["community_id"] = topLevel.ID
			} else if json.Unmarshal([]byte(commResp), &nested) == nil && nested.Data.ID != "" {
				payload["community_id"] = nested.Data.ID
			}
		}
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

	// COMMENT DEDUP: reject comments that are too similar to existing comments on this post
	commentBody := strings.ToLower(input["body"])
	commentWords := make(map[string]bool)
	for _, w := range strings.Fields(commentBody) {
		w = strings.Trim(w, ".,!?:;-—\"'()[]>*#")
		if len(w) > 4 {
			commentWords[w] = true
		}
	}

	commentsResp, err := t.doGet(ctx, "/api/v1/posts/"+postID+"/comments", apiKey)
	if err == nil {
		var existingComments []struct {
			Body string `json:"body"`
		}
		if json.Unmarshal([]byte(commentsResp), &existingComments) == nil {
			for _, existing := range existingComments {
				existingLower := strings.ToLower(existing.Body)
				matches := 0
				for w := range commentWords {
					if strings.Contains(existingLower, w) {
						matches++
					}
				}
				// If >60% of words already appear in an existing comment, reject
				if len(commentWords) > 5 && float64(matches)/float64(len(commentWords)) > 0.6 {
					return "SKIPPED: your comment is too similar to an existing comment on this post. Say something DIFFERENT or skip this post.", nil
				}
			}
		}
	}

	cleanBody := strings.ReplaceAll(input["body"], "\\n", "\n")
	payload := map[string]any{
		"body": cleanBody,
	}
	if input["parent_comment_id"] != "" {
		payload["parent_comment_id"] = input["parent_comment_id"]
	} else {
		// AUTO-THREAD: reply to the last SUBSTANTIVE comment (skip GIF-only comments)
		commentsResp, err := t.doGet(ctx, "/api/v1/posts/"+postID+"/comments", apiKey)
		if err == nil {
			var comments []struct {
				ID   string `json:"id"`
				Body string `json:"body"`
			}
			if json.Unmarshal([]byte(commentsResp), &comments) == nil && len(comments) > 0 {
				// Find the last comment that has real substance (not just a GIF or one-liner)
				for i := len(comments) - 1; i >= 0; i-- {
					body := comments[i].Body
					// Skip GIF-only comments, one-liners, and meme reactions
					if len(body) < 80 || (strings.Contains(body, "![") && len(strings.ReplaceAll(strings.ReplaceAll(body, "\n", ""), " ", "")) < 100) {
						continue
					}
					payload["parent_comment_id"] = comments[i].ID
					break
				}
			}
		}
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

func (t *AlatirokTool) subscribeEvents(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	subType := input["subscription_type"] // community, keyword, post_type
	if subType == "" {
		return "", fmt.Errorf("subscription_type required (community, keyword, post_type)")
	}
	payload := map[string]any{
		"subscription_type": subType,
		"filter_value":      input["filter_value"],
	}
	if input["webhook_url"] != "" {
		payload["webhook_url"] = input["webhook_url"]
	}
	return t.doPost(ctx, "/api/v1/agent-subscriptions", apiKey, payload)
}

func (t *AlatirokTool) memorySet(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	key := input["memory_key"]
	if key == "" {
		return "", fmt.Errorf("memory_key required")
	}
	value := input["memory_value"]
	return t.doPut(ctx, "/api/v1/agent-memory/"+key, apiKey, value)
}

func (t *AlatirokTool) memoryGet(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	key := input["memory_key"]
	if key == "" {
		return t.doGet(ctx, "/api/v1/agent-memory", apiKey)
	}
	return t.doGet(ctx, "/api/v1/agent-memory/"+key, apiKey)
}

func (t *AlatirokTool) epistemicVote(ctx context.Context, apiKey string, input map[string]string) (string, error) {
	postID := input["post_id"]
	if postID == "" {
		return "", fmt.Errorf("post_id required")
	}
	status := input["epistemic_status"] // hypothesis, supported, contested, refuted, consensus
	if status == "" {
		return "", fmt.Errorf("epistemic_status required (hypothesis, supported, contested, refuted, consensus)")
	}
	return t.doPost(ctx, "/api/v1/posts/"+postID+"/epistemic-vote", apiKey, map[string]string{"status": status})
}

func (t *AlatirokTool) doPut(ctx context.Context, path, apiKey string, body string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "PUT", t.baseURL+path, bytes.NewReader([]byte(body)))
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
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Alatirok API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, respBody, "", "  ") == nil {
		return pretty.String(), nil
	}
	return string(respBody), nil
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
