package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	readability "github.com/go-shiori/go-readability"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// WebReaderTool fetches any URL through the configured proxy and extracts the
// main article content as clean markdown. Uses Mozilla's Readability algorithm
// (the same one powering Firefox Reader View) for content extraction, then
// converts the clean HTML to markdown for LLM consumption.
type WebReaderTool struct {
	client  *http.Client
	mu      sync.Mutex
	domains map[string]time.Time // per-domain rate limiting
}

func NewWebReaderTool() *WebReaderTool {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       30 * time.Second,
		MaxIdleConnsPerHost:   5,
	}
	client := &http.Client{
		Timeout:   45 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	return &WebReaderTool{
		client:  client,
		domains: make(map[string]time.Time),
	}
}

func (t *WebReaderTool) Name() string { return "web_reader" }

func (t *WebReaderTool) Description() string {
	return "Fetch a webpage and extract its main article content as clean markdown. Returns title, author, date, and body text. Works with news sites, blogs, research papers, and articles. Use this instead of http_request when you need to READ and understand a webpage."
}

func (t *WebReaderTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"url":            {Type: "string", Description: "The URL to read", Required: true},
		"include_images": {Type: "string", Description: "Include image references in output (true/false, default true)", Required: false},
		"max_length":     {Type: "string", Description: "Maximum output length in characters (default 32768)", Required: false},
	}
}

func (t *WebReaderTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	rawURL := input["url"]
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}

	// Validate URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("URL must be http or https")
	}

	// Per-domain rate limiting — 1 request per 500ms per domain
	domain := parsedURL.Hostname()
	t.mu.Lock()
	lastReq, exists := t.domains[domain]
	if exists && time.Since(lastReq) < 500*time.Millisecond {
		t.mu.Unlock()
		time.Sleep(500*time.Millisecond - time.Since(lastReq))
		t.mu.Lock()
	}
	t.domains[domain] = time.Now()
	t.mu.Unlock()

	// Fetch the page
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CyntrBot/1.0; +https://cyntr.dev)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := t.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("request timed out")
		}
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	// Handle non-200 responses
	if resp.StatusCode != 200 {
		switch {
		case resp.StatusCode == 403:
			return "", fmt.Errorf("access denied (HTTP 403) — site may block automated access")
		case resp.StatusCode == 429:
			return "", fmt.Errorf("rate limited by target site (HTTP 429) — try again later")
		case resp.StatusCode == 402 || resp.StatusCode == 451:
			return "", fmt.Errorf("content behind paywall or restricted (HTTP %d)", resp.StatusCode)
		case resp.StatusCode >= 500:
			return "", fmt.Errorf("server error (HTTP %d)", resp.StatusCode)
		default:
			return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
		}
	}

	// Limit body size to 2MB
	body := io.LimitReader(resp.Body, 2*1024*1024)

	// Extract article using Readability
	article, err := readability.FromReader(body, parsedURL)
	if err != nil {
		// Fallback: try basic text extraction
		return t.fallbackExtract(ctx, rawURL, input)
	}

	// Check if readability found content
	if strings.TrimSpace(article.TextContent) == "" {
		return t.fallbackExtract(ctx, rawURL, input)
	}

	// Convert article HTML to markdown
	markdown, err := htmltomarkdown.ConvertString(article.Content)
	if err != nil {
		// Fallback to plain text if markdown conversion fails
		markdown = article.TextContent
	}

	// Remove images if requested
	includeImages := input["include_images"] != "false"
	if !includeImages {
		lines := strings.Split(markdown, "\n")
		var filtered []string
		for _, line := range lines {
			if !strings.Contains(line, "![") {
				filtered = append(filtered, line)
			}
		}
		markdown = strings.Join(filtered, "\n")
	}

	// Build output with metadata
	var sb strings.Builder
	sb.WriteString("# ")
	sb.WriteString(article.Title)
	sb.WriteString("\n\n")

	// Metadata line
	var meta []string
	if article.SiteName != "" {
		meta = append(meta, "**Source:** "+article.SiteName)
	}
	if article.Byline != "" {
		meta = append(meta, "**Author:** "+article.Byline)
	}
	if article.PublishedTime != nil && !article.PublishedTime.IsZero() {
		meta = append(meta, "**Date:** "+article.PublishedTime.Format("January 2, 2006"))
	}
	if len(meta) > 0 {
		sb.WriteString(strings.Join(meta, " | "))
		sb.WriteString("\n\n---\n\n")
	}

	sb.WriteString(markdown)
	sb.WriteString("\n\n---\n*Source URL: ")
	sb.WriteString(rawURL)
	sb.WriteString("*")

	result := sb.String()

	// Truncate if needed
	maxLen := 32768
	if ml := input["max_length"]; ml != "" {
		fmt.Sscanf(ml, "%d", &maxLen)
	}
	if len(result) > maxLen {
		result = result[:maxLen] + "\n\n...(truncated)"
	}

	return result, nil
}

// fallbackExtract attempts basic text extraction when readability fails
func (t *WebReaderTool) fallbackExtract(ctx context.Context, rawURL string, input map[string]string) (string, error) {
	// Re-fetch the page for fallback
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("fallback fetch failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CyntrBot/1.0)")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fallback fetch failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	// Use basic HTML stripping
	text := htmlTagPattern.ReplaceAllString(string(bodyBytes), "")
	text = strings.TrimSpace(text)

	// Truncate
	maxLen := 32768
	if ml := input["max_length"]; ml != "" {
		fmt.Sscanf(ml, "%d", &maxLen)
	}
	if len(text) > maxLen {
		text = text[:maxLen] + "\n\n...(truncated)"
	}

	return "Note: Could not extract article content. Showing raw page text.\n\n" + text, nil
}
