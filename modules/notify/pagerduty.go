package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultPagerDutyEndpoint = "https://events.pagerduty.com/v2/enqueue"

// PagerDutyChannel sends notifications via the PagerDuty Events API v2.
type PagerDutyChannel struct {
	endpoint   string
	routingKey string
	client     *http.Client
}

// NewPagerDutyChannel creates a PagerDuty Events API v2 channel.
// If endpoint is empty, it defaults to the standard PagerDuty v2 enqueue URL.
func NewPagerDutyChannel(endpoint, routingKey string) *PagerDutyChannel {
	if endpoint == "" {
		endpoint = defaultPagerDutyEndpoint
	}
	return &PagerDutyChannel{
		endpoint:   endpoint,
		routingKey: routingKey,
		client:     &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *PagerDutyChannel) Name() string { return "pagerduty" }

// pagerDutyEvent represents the PagerDuty Events API v2 request body.
type pagerDutyEvent struct {
	RoutingKey  string              `json:"routing_key"`
	EventAction string              `json:"event_action"`
	DedupKey    string              `json:"dedup_key"`
	Payload     *pagerDutyPayload   `json:"payload,omitempty"`
}

type pagerDutyPayload struct {
	Summary       string            `json:"summary"`
	Severity      string            `json:"severity"`
	Source        string            `json:"source"`
	Component     string            `json:"component,omitempty"`
	Group         string            `json:"group,omitempty"`
	CustomDetails map[string]string `json:"custom_details,omitempty"`
}

// mapSeverity converts a Cyntr severity to a PagerDuty severity.
func mapSeverity(severity string) string {
	switch severity {
	case "critical":
		return "critical"
	case "warning":
		return "warning"
	case "error":
		return "error"
	default:
		return "info"
	}
}

// isResolveAction returns true if the notification should resolve an incident.
func isResolveAction(severity string) bool {
	return severity == "info" || severity == "resolved"
}

func (p *PagerDutyChannel) Send(ctx context.Context, n Notification) error {
	dedupKey := fmt.Sprintf("cyntr-%s-%s-%s", n.Tenant, n.Agent, n.Source)

	eventAction := "trigger"
	if isResolveAction(n.Severity) {
		eventAction = "resolve"
	}

	event := pagerDutyEvent{
		RoutingKey:  p.routingKey,
		EventAction: eventAction,
		DedupKey:    dedupKey,
	}

	// PagerDuty requires a payload for trigger actions; resolve only needs
	// routing_key, event_action, and dedup_key, but including payload is fine.
	summary := n.Title
	if n.Message != "" {
		summary = n.Title + ": " + n.Message
	}

	event.Payload = &pagerDutyPayload{
		Summary:       summary,
		Severity:      mapSeverity(n.Severity),
		Source:        n.Source,
		Component:     n.Agent,
		Group:         n.Tenant,
		CustomDetails: n.Fields,
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("pagerduty marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("pagerduty request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("pagerduty send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("pagerduty response: %d", resp.StatusCode)
	}
	return nil
}
