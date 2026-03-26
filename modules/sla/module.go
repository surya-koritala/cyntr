package sla

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/notify"
)

var logger = log.Default().WithModule("sla")

// Monitor tracks SLA rules and detects violations.
type Monitor struct {
	mu         sync.Mutex
	bus        *ipc.Bus
	notifier   *notify.Notifier
	rules      []Rule
	violations []Violation
	latencies  map[string][]latencyRecord // "tenant/agent" → records
	errors     map[string][]time.Time     // "tenant/agent" → error timestamps
	counter    int64
}

func New() *Monitor {
	return &Monitor{
		latencies: make(map[string][]latencyRecord),
		errors:    make(map[string][]time.Time),
	}
}

func (m *Monitor) Name() string           { return "sla" }
func (m *Monitor) Dependencies() []string { return []string{"agent_runtime"} }

func (m *Monitor) SetNotifier(n *notify.Notifier) { m.notifier = n }

func (m *Monitor) Init(ctx context.Context, svc *kernel.Services) error {
	m.bus = svc.Bus
	return nil
}

func (m *Monitor) Start(ctx context.Context) error {
	m.bus.Handle("sla", "sla.add_rule", m.handleAddRule)
	m.bus.Handle("sla", "sla.list_rules", m.handleListRules)
	m.bus.Handle("sla", "sla.remove_rule", m.handleRemoveRule)
	m.bus.Handle("sla", "sla.violations", m.handleListViolations)

	go m.runChecker(ctx)

	return nil
}

func (m *Monitor) Stop(ctx context.Context) error { return nil }

func (m *Monitor) Health(ctx context.Context) kernel.HealthStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	active := 0
	for _, r := range m.rules {
		if r.Enabled {
			active++
		}
	}
	return kernel.HealthStatus{
		Healthy: true,
		Message: fmt.Sprintf("%d rules, %d violations", active, len(m.violations)),
	}
}

// AddRule registers an SLA rule.
func (m *Monitor) AddRule(rule Rule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rule.WindowMinutes == 0 {
		rule.WindowMinutes = 5
	}
	if rule.ID == "" {
		m.counter++
		rule.ID = fmt.Sprintf("sla_%d", m.counter)
	}
	m.rules = append(m.rules, rule)
}

// RecordResponse records an agent response time for SLA evaluation.
func (m *Monitor) RecordResponse(agentName, tenant string, durationMs int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := tenant + "/" + agentName
	m.latencies[key] = append(m.latencies[key], latencyRecord{
		DurationMs: durationMs, Timestamp: time.Now(),
	})
}

// RecordError records an agent error for error rate evaluation.
func (m *Monitor) RecordError(agentName, tenant string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := tenant + "/" + agentName
	m.errors[key] = append(m.errors[key], time.Now())
}

// Check evaluates all enabled rules and returns new violations.
func (m *Monitor) Check() []Violation {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var newViolations []Violation

	for _, rule := range m.rules {
		if !rule.Enabled {
			continue
		}
		window := time.Duration(rule.WindowMinutes) * time.Minute
		key := rule.Tenant + "/" + rule.Agent

		// Check latency SLA
		if rule.MaxResponseMs > 0 {
			records := m.latencies[key]
			var recent []latencyRecord
			var totalMs int64
			for _, r := range records {
				if now.Sub(r.Timestamp) <= window {
					recent = append(recent, r)
					totalMs += r.DurationMs
				}
			}
			m.latencies[key] = recent // prune expired

			if len(recent) > 0 {
				avgMs := totalMs / int64(len(recent))
				if avgMs > rule.MaxResponseMs {
					m.counter++
					newViolations = append(newViolations, Violation{
						ID:        fmt.Sprintf("vio_%d", m.counter),
						RuleID:    rule.ID,
						Agent:     rule.Agent,
						Tenant:    rule.Tenant,
						Type:      "latency",
						Value:     float64(avgMs),
						Threshold: float64(rule.MaxResponseMs),
						Timestamp: now,
					})
				}
			}
		}

		// Check error rate SLA
		if rule.MaxErrorRate > 0 {
			errTimes := m.errors[key]
			var recentErrors []time.Time
			for _, t := range errTimes {
				if now.Sub(t) <= window {
					recentErrors = append(recentErrors, t)
				}
			}
			m.errors[key] = recentErrors

			latRecords := m.latencies[key]
			totalCalls := len(latRecords) + len(recentErrors)
			if totalCalls > 0 {
				errorRate := float64(len(recentErrors)) / float64(totalCalls) * 100
				if errorRate > rule.MaxErrorRate {
					m.counter++
					newViolations = append(newViolations, Violation{
						ID:        fmt.Sprintf("vio_%d", m.counter),
						RuleID:    rule.ID,
						Agent:     rule.Agent,
						Tenant:    rule.Tenant,
						Type:      "error_rate",
						Value:     errorRate,
						Threshold: rule.MaxErrorRate,
						Timestamp: now,
					})
				}
			}
		}
	}

	m.violations = append(m.violations, newViolations...)
	return newViolations
}

// Violations returns all recorded violations.
func (m *Monitor) Violations() []Violation {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := make([]Violation, len(m.violations))
	copy(v, m.violations)
	return v
}

func (m *Monitor) runChecker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			violations := m.Check()
			for _, v := range violations {
				logger.Warn("SLA violation", map[string]any{
					"rule": v.RuleID, "agent": v.Agent, "type": v.Type,
					"value": v.Value, "threshold": v.Threshold,
				})
				if m.notifier != nil {
					m.notifier.Send(context.Background(), notify.Notification{
						Title:    fmt.Sprintf("SLA Breach: %s/%s", v.Tenant, v.Agent),
						Message:  fmt.Sprintf("%s: %.0f (threshold: %.0f)", v.Type, v.Value, v.Threshold),
						Severity: "warning",
						Agent:    v.Agent,
						Tenant:   v.Tenant,
						Source:   "sla-monitor",
					})
				}
			}
		}
	}
}

func (m *Monitor) handleAddRule(msg ipc.Message) (ipc.Message, error) {
	rule, ok := msg.Payload.(Rule)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected Rule, got %T", msg.Payload)
	}
	m.AddRule(rule)
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "ok"}, nil
}

func (m *Monitor) handleListRules(msg ipc.Message) (ipc.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rules := make([]Rule, len(m.rules))
	copy(rules, m.rules)
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: rules}, nil
}

func (m *Monitor) handleRemoveRule(msg ipc.Message) (ipc.Message, error) {
	ruleID, _ := msg.Payload.(string)
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.rules {
		if r.ID == ruleID {
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			return ipc.Message{Type: ipc.MessageTypeResponse, Payload: "removed"}, nil
		}
	}
	return ipc.Message{}, fmt.Errorf("rule %q not found", ruleID)
}

func (m *Monitor) handleListViolations(msg ipc.Message) (ipc.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := make([]Violation, len(m.violations))
	copy(v, m.violations)
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: v}, nil
}
