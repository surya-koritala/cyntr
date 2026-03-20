package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// BrowserTool fetches web pages and extracts readable text content.
type BrowserTool struct {
	client *http.Client
}

func NewBrowserTool() *BrowserTool {
	return &BrowserTool{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *BrowserTool) Name() string        { return "browse_web" }
func (t *BrowserTool) Description() string { return "Fetch a web page and extract its text content" }
func (t *BrowserTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"url": {Type: "string", Description: "The URL to fetch", Required: true},
	}
}

func (t *BrowserTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	url := input["url"]
	if url == "" {
		return "", fmt.Errorf("url is required")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Cyntr/0.1.0 (Enterprise AI Agent)")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB limit
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}

	text := extractText(string(body))

	// Truncate to 32KB for model context
	if len(text) > 32768 {
		text = text[:32768] + "\n... (truncated)"
	}

	return fmt.Sprintf("URL: %s\nStatus: %d\n\n%s", url, resp.StatusCode, text), nil
}

// extractText removes HTML tags and extracts readable text.
func extractText(html string) string {
	// Remove script and style blocks
	scriptRe := regexp.MustCompile(`(?is)<script.*?</script>`)
	html = scriptRe.ReplaceAllString(html, "")
	styleRe := regexp.MustCompile(`(?is)<style.*?</style>`)
	html = styleRe.ReplaceAllString(html, "")

	// Remove HTML tags
	tagRe := regexp.MustCompile(`<[^>]*>`)
	text := tagRe.ReplaceAllString(html, " ")

	// Decode common HTML entities
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", `"`)
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = strings.ReplaceAll(text, "&nbsp;", " ")

	// Collapse whitespace
	spaceRe := regexp.MustCompile(`\s+`)
	text = spaceRe.ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}
