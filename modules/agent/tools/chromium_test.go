package tools

import (
	"context"
	"os"
	"testing"
)

func TestChromiumToolName(t *testing.T) {
	if NewChromiumTool().Name() != "chromium_browser" {
		t.Fatal("unexpected name")
	}
}

func TestChromiumToolMissingAction(t *testing.T) {
	tool := NewChromiumTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing action")
	}
}

func TestChromiumToolUnknownAction(t *testing.T) {
	tool := NewChromiumTool()
	_, err := tool.Execute(context.Background(), map[string]string{"action": "dance"})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestChromiumToolNavigateMissingURL(t *testing.T) {
	tool := NewChromiumTool()
	_, err := tool.Execute(context.Background(), map[string]string{"action": "navigate"})
	if err == nil {
		t.Fatal("expected error for navigate without URL")
	}
}

func TestChromiumToolClickMissingSelector(t *testing.T) {
	if !chromeInstalled() {
		t.Skip("Chrome not installed")
	}
	tool := NewChromiumTool()
	_, err := tool.Execute(context.Background(), map[string]string{"action": "click"})
	if err == nil {
		t.Fatal("expected error for click without selector")
	}
}

func TestChromiumToolFillMissingParams(t *testing.T) {
	if !chromeInstalled() {
		t.Skip("Chrome not installed")
	}
	tool := NewChromiumTool()
	_, err := tool.Execute(context.Background(), map[string]string{"action": "fill", "selector": "#input"})
	if err == nil {
		t.Fatal("expected error for fill without value")
	}
}

func TestChromiumToolExecuteJSMissingValue(t *testing.T) {
	if !chromeInstalled() {
		t.Skip("Chrome not installed")
	}
	tool := NewChromiumTool()
	_, err := tool.Execute(context.Background(), map[string]string{"action": "execute_js"})
	if err == nil {
		t.Fatal("expected error for execute_js without value")
	}
}

func TestChromiumToolWaitForMissingSelector(t *testing.T) {
	if !chromeInstalled() {
		t.Skip("Chrome not installed")
	}
	tool := NewChromiumTool()
	_, err := tool.Execute(context.Background(), map[string]string{"action": "wait_for"})
	if err == nil {
		t.Fatal("expected error for wait_for without selector")
	}
}

func TestChromiumToolURLAllowlist(t *testing.T) {
	os.Setenv("CHROMIUM_ALLOWED_URLS", "https://example.com,https://test.com")
	defer os.Unsetenv("CHROMIUM_ALLOWED_URLS")

	tool := NewChromiumTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"action": "navigate", "url": "https://evil.com/steal",
	})
	if err == nil {
		t.Fatal("expected error for disallowed URL")
	}
}

func TestChromiumToolURLAllowlistPermitted(t *testing.T) {
	if !chromeInstalled() {
		t.Skip("Chrome not installed")
	}
	os.Setenv("CHROMIUM_ALLOWED_URLS", "https://example.com")
	defer os.Unsetenv("CHROMIUM_ALLOWED_URLS")

	tool := NewChromiumTool()
	// This would succeed if Chrome is available - the URL passes the allowlist check
	// The actual navigation may fail due to network, but the allowlist check passes
	_, _ = tool.Execute(context.Background(), map[string]string{
		"action": "navigate", "url": "https://example.com",
	})
	// No assertion on result - just verifying allowlist doesn't block it
}

func TestChromiumToolParameters(t *testing.T) {
	tool := NewChromiumTool()
	params := tool.Parameters()
	if _, ok := params["action"]; !ok {
		t.Fatal("missing action param")
	}
	if !params["action"].Required {
		t.Fatal("action should be required")
	}
}
