package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type AdvancedBrowserTool struct {
	client *http.Client
}

func NewAdvancedBrowserTool() *AdvancedBrowserTool {
	return &AdvancedBrowserTool{client: &http.Client{Timeout: 30 * time.Second}}
}

func (t *AdvancedBrowserTool) Name() string { return "advanced_browser" }
func (t *AdvancedBrowserTool) Description() string {
	return "Advanced web browser: navigate, extract elements by CSS selector, submit forms, get page links"
}
func (t *AdvancedBrowserTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"action":   {Type: "string", Description: "Action: get, extract, links, form_submit", Required: true},
		"url":      {Type: "string", Description: "URL to navigate to", Required: true},
		"selector": {Type: "string", Description: "CSS-like selector for extract (e.g., 'h1', 'p', 'a', '.class', '#id')", Required: false},
		"data":     {Type: "string", Description: "Form data as key=value&key2=value2 (for form_submit)", Required: false},
	}
}

func (t *AdvancedBrowserTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	action := input["action"]
	rawURL := input["url"]
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}

	switch action {
	case "get":
		return t.fetchPage(ctx, rawURL)
	case "extract":
		return t.extractElements(ctx, rawURL, input["selector"])
	case "links":
		return t.extractLinks(ctx, rawURL)
	case "form_submit":
		return t.submitForm(ctx, rawURL, input["data"])
	default:
		return "", fmt.Errorf("unknown action: %s (supported: get, extract, links, form_submit)", action)
	}
}

func (t *AdvancedBrowserTool) fetchPage(ctx context.Context, rawURL string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	req.Header.Set("User-Agent", "Cyntr/0.3.0")
	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	text := extractText(string(body))
	if len(text) > 32768 {
		text = text[:32768] + "\n...(truncated)"
	}
	return fmt.Sprintf("URL: %s\nStatus: %d\n\n%s", rawURL, resp.StatusCode, text), nil
}

func (t *AdvancedBrowserTool) extractElements(ctx context.Context, rawURL, selector string) (string, error) {
	if selector == "" {
		return "", fmt.Errorf("selector is required for extract action")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	req.Header.Set("User-Agent", "Cyntr/0.3.0")
	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	html := string(body)

	// Simple CSS-like selector matching
	var pattern string
	if strings.HasPrefix(selector, ".") {
		// Class selector
		class := selector[1:]
		pattern = fmt.Sprintf(`(?i)<[^>]*class="[^"]*%s[^"]*"[^>]*>(.*?)</`, regexp.QuoteMeta(class))
	} else if strings.HasPrefix(selector, "#") {
		// ID selector
		id := selector[1:]
		pattern = fmt.Sprintf(`(?i)<[^>]*id="%s"[^>]*>(.*?)</`, regexp.QuoteMeta(id))
	} else {
		// Tag selector
		pattern = fmt.Sprintf(`(?i)<%s[^>]*>(.*?)</%s>`, regexp.QuoteMeta(selector), regexp.QuoteMeta(selector))
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid selector pattern: %w", err)
	}

	matches := re.FindAllStringSubmatch(html, 50)
	if len(matches) == 0 {
		return "No elements found matching: " + selector, nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Found %d elements matching '%s':\n\n", len(matches), selector))
	for i, m := range matches {
		if i >= 20 {
			result.WriteString("...(more results truncated)\n")
			break
		}
		text := extractText(m[1])
		if text != "" {
			result.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(text)))
		}
	}
	return result.String(), nil
}

func (t *AdvancedBrowserTool) extractLinks(ctx context.Context, rawURL string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	req.Header.Set("User-Agent", "Cyntr/0.3.0")
	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	re := regexp.MustCompile(`(?i)<a[^>]+href="([^"]+)"[^>]*>(.*?)</a>`)
	matches := re.FindAllStringSubmatch(string(body), 100)

	if len(matches) == 0 {
		return "No links found on page.", nil
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("Found %d links:\n\n", len(matches)))
	for i, m := range matches {
		if i >= 50 {
			break
		}
		href := m[1]
		text := strings.TrimSpace(extractText(m[2]))
		if text == "" {
			text = "(no text)"
		}
		// Resolve relative URLs
		if !strings.HasPrefix(href, "http") {
			base, _ := url.Parse(rawURL)
			ref, _ := url.Parse(href)
			if base != nil && ref != nil {
				href = base.ResolveReference(ref).String()
			}
		}
		result.WriteString(fmt.Sprintf("- [%s](%s)\n", text, href))
	}
	return result.String(), nil
}

func (t *AdvancedBrowserTool) submitForm(ctx context.Context, rawURL, data string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "POST", rawURL, strings.NewReader(data))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Cyntr/0.3.0")
	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	text := extractText(string(body))
	if len(text) > 16384 {
		text = text[:16384]
	}
	return fmt.Sprintf("Form submitted to %s\nStatus: %d\n\n%s", rawURL, resp.StatusCode, text), nil
}
