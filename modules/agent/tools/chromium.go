package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

const chromiumTimeout = 30 * time.Second

type ChromiumTool struct{}

func NewChromiumTool() *ChromiumTool { return &ChromiumTool{} }

func (t *ChromiumTool) Name() string { return "chromium_browser" }
func (t *ChromiumTool) Description() string {
	return "Control a headless Chromium browser. Actions: navigate, click, fill, screenshot, extract_text, execute_js, wait_for."
}
func (t *ChromiumTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"action":   {Type: "string", Description: "Action: navigate, click, fill, screenshot, extract_text, execute_js, wait_for", Required: true},
		"url":      {Type: "string", Description: "URL to navigate to (for navigate action)", Required: false},
		"selector": {Type: "string", Description: "CSS selector for click, fill, extract_text, wait_for", Required: false},
		"value":    {Type: "string", Description: "Value for fill action or JS code for execute_js", Required: false},
	}
}

func (t *ChromiumTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	action := input["action"]
	if action == "" {
		return "", fmt.Errorf("action is required")
	}

	// Check URL allowlist
	if action == "navigate" {
		url := input["url"]
		if url == "" {
			return "", fmt.Errorf("url is required for navigate action")
		}
		if allowedURLs := os.Getenv("CHROMIUM_ALLOWED_URLS"); allowedURLs != "" {
			allowed := false
			for _, prefix := range strings.Split(allowedURLs, ",") {
				prefix = strings.TrimSpace(prefix)
				if prefix != "" && strings.HasPrefix(url, prefix) {
					allowed = true
					break
				}
			}
			if !allowed {
				return "", fmt.Errorf("URL %q not in allowlist (CHROMIUM_ALLOWED_URLS)", url)
			}
		}
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, chromiumTimeout)
	defer cancel()

	allocCtx, allocCancel := chromedp.NewExecAllocator(timeoutCtx,
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
		)...,
	)
	defer allocCancel()

	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	switch action {
	case "navigate":
		url := input["url"]
		var title string
		err := chromedp.Run(taskCtx,
			chromedp.Navigate(url),
			chromedp.Title(&title),
		)
		if err != nil {
			return "", fmt.Errorf("navigate: %w", err)
		}
		return fmt.Sprintf("Navigated to: %s\nTitle: %s", url, title), nil

	case "click":
		sel := input["selector"]
		if sel == "" {
			return "", fmt.Errorf("selector is required for click action")
		}
		err := chromedp.Run(taskCtx,
			chromedp.Click(sel, chromedp.ByQuery),
		)
		if err != nil {
			return "", fmt.Errorf("click: %w", err)
		}
		return fmt.Sprintf("Clicked: %s", sel), nil

	case "fill":
		sel := input["selector"]
		val := input["value"]
		if sel == "" || val == "" {
			return "", fmt.Errorf("selector and value are required for fill action")
		}
		err := chromedp.Run(taskCtx,
			chromedp.Clear(sel, chromedp.ByQuery),
			chromedp.SendKeys(sel, val, chromedp.ByQuery),
		)
		if err != nil {
			return "", fmt.Errorf("fill: %w", err)
		}
		return fmt.Sprintf("Filled %s with value", sel), nil

	case "screenshot":
		var buf []byte
		err := chromedp.Run(taskCtx,
			chromedp.CaptureScreenshot(&buf),
		)
		if err != nil {
			return "", fmt.Errorf("screenshot: %w", err)
		}
		encoded := base64.StdEncoding.EncodeToString(buf)
		return fmt.Sprintf("Screenshot captured (%d bytes)\nBase64: %s", len(buf), encoded[:min(200, len(encoded))]), nil

	case "extract_text":
		sel := input["selector"]
		if sel == "" {
			sel = "body"
		}
		var text string
		err := chromedp.Run(taskCtx,
			chromedp.Text(sel, &text, chromedp.ByQuery),
		)
		if err != nil {
			return "", fmt.Errorf("extract_text: %w", err)
		}
		if len(text) > 65536 {
			text = text[:65536] + "\n[Truncated at 64KB]"
		}
		return text, nil

	case "execute_js":
		js := input["value"]
		if js == "" {
			return "", fmt.Errorf("value (JS code) is required for execute_js action")
		}
		var result any
		err := chromedp.Run(taskCtx,
			chromedp.Evaluate(js, &result),
		)
		if err != nil {
			return "", fmt.Errorf("execute_js: %w", err)
		}
		return fmt.Sprintf("%v", result), nil

	case "wait_for":
		sel := input["selector"]
		if sel == "" {
			return "", fmt.Errorf("selector is required for wait_for action")
		}
		err := chromedp.Run(taskCtx,
			chromedp.WaitVisible(sel, chromedp.ByQuery),
		)
		if err != nil {
			return "", fmt.Errorf("wait_for: %w", err)
		}
		return fmt.Sprintf("Element visible: %s", sel), nil

	default:
		return "", fmt.Errorf("unknown action %q: use navigate, click, fill, screenshot, extract_text, execute_js, wait_for", action)
	}
}

// chromeInstalled returns true if Chrome or Chromium is available on the system.
func chromeInstalled() bool {
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}
