package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GenericWebhookChannel sends notifications as JSON to an arbitrary HTTP endpoint.
type GenericWebhookChannel struct {
	name    string
	url     string
	headers map[string]string
	client  *http.Client

	// MaxRetries is the maximum number of retry attempts after the initial request.
	// Default is 1.
	MaxRetries int

	// RetryDelayMs is the delay in milliseconds between retry attempts.
	// Default is 1000.
	RetryDelayMs int
}

// NewGenericWebhookChannel creates a new generic webhook channel.
func NewGenericWebhookChannel(name, url string, headers map[string]string) *GenericWebhookChannel {
	return &GenericWebhookChannel{
		name:         name,
		url:          url,
		headers:      headers,
		client:       &http.Client{Timeout: 15 * time.Second},
		MaxRetries:   1,
		RetryDelayMs: 1000,
	}
}

// Name returns the configured channel name.
func (g *GenericWebhookChannel) Name() string { return g.name }

// Send posts the notification as a JSON payload to the configured URL.
// It retries on HTTP status >= 400 or network errors up to MaxRetries times.
func (g *GenericWebhookChannel) Send(ctx context.Context, n Notification) error {
	payload := map[string]string{
		"title":     n.Title,
		"message":   n.Message,
		"severity":  n.Severity,
		"agent":     n.Agent,
		"tenant":    n.Tenant,
		"source":    n.Source,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("generic_webhook: marshal payload: %w", err)
	}

	attempts := 1 + g.MaxRetries
	var lastErr error

	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(g.RetryDelayMs) * time.Millisecond):
			}
		}

		lastErr = g.doRequest(ctx, body)
		if lastErr == nil {
			return nil
		}
	}

	return fmt.Errorf("generic_webhook: %d attempts exhausted: %w", attempts, lastErr)
}

func (g *GenericWebhookChannel) doRequest(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range g.headers {
		req.Header.Set(k, v)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
