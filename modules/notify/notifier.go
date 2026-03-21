package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/log"
)

var logger = log.Default().WithModule("notify")

// NotificationType categorizes the notification.
type NotificationType string

const (
	NotifyApproval NotificationType = "approval_needed"
	NotifyDenied   NotificationType = "policy_denied"
	NotifyError    NotificationType = "error"
	NotifyInfo     NotificationType = "info"
)

// Notification represents an alert to send.
type Notification struct {
	Type    NotificationType
	Title   string
	Message string
	Tenant  string
	Fields  map[string]string
}

// Channel is a notification delivery mechanism.
type Channel interface {
	Name() string
	Send(ctx context.Context, n Notification) error
}

// SlackWebhookChannel sends notifications via Slack incoming webhook.
type SlackWebhookChannel struct {
	webhookURL string
	client     *http.Client
}

func NewSlackWebhook(webhookURL string) *SlackWebhookChannel {
	return &SlackWebhookChannel{webhookURL: webhookURL, client: &http.Client{Timeout: 10 * time.Second}}
}

func (s *SlackWebhookChannel) Name() string { return "slack_webhook" }

func (s *SlackWebhookChannel) Send(ctx context.Context, n Notification) error {
	emoji := "ℹ️"
	switch n.Type {
	case NotifyApproval:
		emoji = "🔔"
	case NotifyDenied:
		emoji = "🚫"
	case NotifyError:
		emoji = "❌"
	}

	text := fmt.Sprintf("%s *%s*\n%s", emoji, n.Title, n.Message)
	if n.Tenant != "" {
		text += fmt.Sprintf("\nTenant: %s", n.Tenant)
	}
	for k, v := range n.Fields {
		text += fmt.Sprintf("\n%s: %s", k, v)
	}

	payload := map[string]string{"text": text}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequestWithContext(ctx, "POST", s.webhookURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack webhook %d", resp.StatusCode)
	}
	return nil
}

// LogChannel writes notifications to stdout (always enabled).
type LogChannel struct{}

func (l *LogChannel) Name() string { return "log" }
func (l *LogChannel) Send(ctx context.Context, n Notification) error {
	logger.Info("notification", map[string]any{"type": string(n.Type), "title": n.Title, "tenant": n.Tenant})
	return nil
}

// Notifier manages notification channels and dispatches alerts.
type Notifier struct {
	mu       sync.RWMutex
	channels []Channel
}

func NewNotifier() *Notifier {
	return &Notifier{channels: []Channel{&LogChannel{}}} // log always enabled
}

// AddChannel registers a notification channel.
func (n *Notifier) AddChannel(ch Channel) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.channels = append(n.channels, ch)
}

// Send dispatches a notification to all channels.
func (n *Notifier) Send(ctx context.Context, notif Notification) {
	n.mu.RLock()
	channels := make([]Channel, len(n.channels))
	copy(channels, n.channels)
	n.mu.RUnlock()

	for _, ch := range channels {
		go func(c Channel) {
			if err := c.Send(ctx, notif); err != nil {
				logger.Warn("notification delivery failed", map[string]any{
					"channel": c.Name(), "type": string(notif.Type), "error": err.Error(),
				})
			}
		}(ch)
	}
}

// ChannelCount returns the number of registered channels.
func (n *Notifier) ChannelCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return len(n.channels)
}
