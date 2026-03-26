package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultDatadogEndpoint = "https://api.datadoghq.com"

// DatadogChannel sends notifications as Datadog Events and supports pushing metrics.
type DatadogChannel struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

// NewDatadogChannel creates a new Datadog channel. If endpoint is empty, it
// defaults to https://api.datadoghq.com.
func NewDatadogChannel(endpoint, apiKey string) *DatadogChannel {
	if endpoint == "" {
		endpoint = defaultDatadogEndpoint
	}
	return &DatadogChannel{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (d *DatadogChannel) Name() string { return "datadog" }

// Send posts a Notification as a Datadog Event to /api/v1/events.
func (d *DatadogChannel) Send(ctx context.Context, n Notification) error {
	alertType := severityToAlertType(n.Severity)

	tags := []string{}
	if n.Agent != "" {
		tags = append(tags, "agent:"+n.Agent)
	}
	if n.Tenant != "" {
		tags = append(tags, "tenant:"+n.Tenant)
	}
	if n.Source != "" {
		tags = append(tags, "source:"+n.Source)
	}

	event := map[string]any{
		"title":            n.Title,
		"text":             n.Message,
		"alert_type":       alertType,
		"source_type_name": "cyntr",
		"tags":             tags,
	}

	return d.post(ctx, "/api/v1/events", event)
}

// SendMetric pushes a gauge metric to Datadog via /api/v1/series.
func (d *DatadogChannel) SendMetric(ctx context.Context, metric string, value float64, tags map[string]string) error {
	tagList := make([]string, 0, len(tags))
	for k, v := range tags {
		tagList = append(tagList, k+":"+v)
	}

	now := time.Now().Unix()

	payload := map[string]any{
		"series": []map[string]any{
			{
				"metric": metric,
				"type":   "gauge",
				"points": [][]any{{float64(now), value}},
				"tags":   tagList,
			},
		},
	}

	return d.post(ctx, "/api/v1/series", payload)
}

// post is a DRY helper that marshals payload to JSON, sets the DD-API-KEY
// header, and POSTs to the given path.
func (d *DatadogChannel) post(ctx context.Context, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("datadog marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("datadog request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DD-API-KEY", d.apiKey)

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("datadog post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("datadog %s responded %d", path, resp.StatusCode)
	}
	return nil
}

// severityToAlertType maps a Cyntr severity string to a Datadog alert_type.
func severityToAlertType(severity string) string {
	switch severity {
	case "critical", "error":
		return "error"
	case "warning":
		return "warning"
	default:
		return "info"
	}
}
