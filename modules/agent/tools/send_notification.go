package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type SendNotificationTool struct {
	client *http.Client
}

func NewSendNotificationTool() *SendNotificationTool {
	return &SendNotificationTool{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (t *SendNotificationTool) Name() string { return "send_notification" }
func (t *SendNotificationTool) Description() string {
	return "Send a notification to Slack webhook, Microsoft Teams webhook, or a generic webhook URL. Use for alerts, status updates, and report delivery."
}
func (t *SendNotificationTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"webhook_url": {Type: "string", Description: "Webhook URL (Slack incoming webhook, Teams connector, or any URL)", Required: true},
		"title":       {Type: "string", Description: "Notification title/subject", Required: true},
		"message":     {Type: "string", Description: "Notification body text", Required: true},
		"severity":    {Type: "string", Description: "Severity: info, warning, critical (default: info)", Required: false},
	}
}

func (t *SendNotificationTool) SetHTTPClient(c *http.Client) { t.client = c }

func (t *SendNotificationTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	webhookURL := input["webhook_url"]
	title := input["title"]
	message := input["message"]

	if webhookURL == "" || title == "" || message == "" {
		return "", fmt.Errorf("webhook_url, title, and message are required")
	}

	severity := input["severity"]
	if severity == "" {
		severity = "info"
	}

	// Format for Slack-compatible webhooks
	severityPrefix := map[string]string{"info": "[INFO]", "warning": "[WARNING]", "critical": "[CRITICAL]"}
	prefix := severityPrefix[severity]
	if prefix == "" {
		prefix = "[INFO]"
	}

	payload := map[string]string{
		"text": fmt.Sprintf("%s *%s*\n%s", prefix, title, message),
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("send notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("webhook returned %d", resp.StatusCode)
	}

	return fmt.Sprintf("Notification sent: [%s] %s", severity, title), nil
}
