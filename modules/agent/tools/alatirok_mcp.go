package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// AlatirokMCPTool interacts with Alatirok via their native MCP REST gateway.
// Uses the 59 MCP tools directly, with built-in dedup tracking.
type AlatirokMCPTool struct {
	client    *http.Client
	baseURL   string
	mu        sync.Mutex
	postedTitles map[string]bool // track posted titles to prevent duplicates
}

func NewAlatirokMCPTool() *AlatirokMCPTool {
	baseURL := os.Getenv("ALATIROK_BASE_URL")
	if baseURL == "" {
		baseURL = "https://www.alatirok.com"
	}
	return &AlatirokMCPTool{
		client:       &http.Client{Timeout: 30 * time.Second},
		baseURL:      strings.TrimRight(baseURL, "/"),
		postedTitles: make(map[string]bool),
	}
}

func (t *AlatirokMCPTool) Name() string { return "alatirok" }
func (t *AlatirokMCPTool) Description() string {
	return "Interact with Alatirok via MCP. 59 tools: create_post, get_post, get_feed, create_comment, get_comments, vote, vote_epistemic, search, list_communities, join_community, store_memory, recall_memory, endorse_agent, get_leaderboard, list_tasks, claim_task, send_message, get_notifications, heartbeat, and more."
}

func (t *AlatirokMCPTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"tool":  {Type: "string", Description: "MCP tool name (e.g. create_post, get_feed, vote, vote_epistemic, create_comment, search, list_communities, join_community, endorse_agent, store_memory, recall_memory, heartbeat, get_notifications, send_message, list_tasks, claim_task, get_leaderboard, get_stats, whoami)", Required: true},
		"input": {Type: "string", Description: "JSON object with tool parameters (e.g. {\"sort\":\"hot\",\"limit\":\"10\"} for get_feed)", Required: true},
	}
}

func (t *AlatirokMCPTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	// Get API key from agent secrets or env
	apiKey := input["api_key"]
	if apiKey == "" {
		apiKey = input["alatirok_api_key"]
	}
	if apiKey == "" {
		apiKey = os.Getenv("ALATIROK_API_KEY")
	}
	if apiKey == "" {
		return "", fmt.Errorf("ALATIROK_API_KEY not set")
	}

	toolName := input["tool"]
	if toolName == "" {
		return "", fmt.Errorf("tool name is required")
	}

	// Parse input JSON
	toolInput := input["input"]
	if toolInput == "" {
		toolInput = "{}"
	}
	// Fix newlines in JSON strings — LLMs put literal newlines in JSON values
	toolInput = strings.ReplaceAll(toolInput, "\n", "\\n")
	toolInput = strings.ReplaceAll(toolInput, "\r", "")
	toolInput = strings.ReplaceAll(toolInput, "\\\\n", "\\n") // don't double-escape

	var inputMap map[string]any
	if err := json.Unmarshal([]byte(toolInput), &inputMap); err != nil {
		// Try to salvage — wrap in object if it's a simple value
		return "", fmt.Errorf("invalid input JSON: %w (input was: %s)", err, toolInput[:min(100, len(toolInput))])
	}

	// Convert escaped newlines back to real newlines in string values for post body
	for k, v := range inputMap {
		if s, ok := v.(string); ok {
			inputMap[k] = strings.ReplaceAll(s, "\\n", "\n")
		}
	}

	// Dedup check for create_post
	if toolName == "create_post" {
		if title, ok := inputMap["title"].(string); ok {
			t.mu.Lock()
			key := strings.ToLower(title[:min(40, len(title))])
			if t.postedTitles[key] {
				t.mu.Unlock()
				return "SKIPPED: similar post already created this session", nil
			}
			t.postedTitles[key] = true
			t.mu.Unlock()
		}
	}

	// Send heartbeat
	go t.heartbeat(apiKey)

	// Call MCP
	return t.callMCP(ctx, apiKey, toolName, inputMap)
}

func (t *AlatirokMCPTool) callMCP(ctx context.Context, apiKey, toolName string, input map[string]any) (string, error) {
	payload := map[string]any{
		"tool":  toolName,
		"input": input,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", t.baseURL+"/mcp/tools/call", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("MCP error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	// Pretty-print
	var pretty bytes.Buffer
	if json.Indent(&pretty, respBody, "", "  ") == nil {
		return pretty.String(), nil
	}
	return string(respBody), nil
}

func (t *AlatirokMCPTool) heartbeat(apiKey string) {
	payload := map[string]any{"tool": "heartbeat", "input": map[string]any{}}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", t.baseURL+"/mcp/tools/call", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}
