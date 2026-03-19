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

type HTTPTool struct {
	client *http.Client
}

func NewHTTPTool() *HTTPTool {
	return &HTTPTool{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *HTTPTool) Name() string        { return "http_request" }
func (t *HTTPTool) Description() string { return "Make an HTTP request and return the response" }
func (t *HTTPTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"url":     {Type: "string", Description: "The URL to request", Required: true},
		"method":  {Type: "string", Description: "HTTP method (GET, POST, etc.)", Required: false},
		"body":    {Type: "string", Description: "Request body", Required: false},
		"headers": {Type: "string", Description: "JSON object of headers", Required: false},
	}
}

func (t *HTTPTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	url := input["url"]
	if url == "" {
		return "", fmt.Errorf("url is required")
	}

	method := input["method"]
	if method == "" {
		method = "GET"
	}

	var bodyReader io.Reader
	if input["body"] != "" {
		bodyReader = bytes.NewBufferString(input["body"])
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	if input["headers"] != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(input["headers"]), &headers); err == nil {
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		}
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	result := fmt.Sprintf("Status: %d\n\n%s", resp.StatusCode, string(body))
	return result, nil
}
