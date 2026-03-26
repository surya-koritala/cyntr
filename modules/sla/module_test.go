package sla

import (
	"testing"
	"time"
)

func TestSLACheckLatencyBreach(t *testing.T) {
	m := New()
	m.AddRule(Rule{
		ID: "r1", Agent: "bot", Tenant: "ops",
		MaxResponseMs: 5000, WindowMinutes: 5, Enabled: true,
	})

	m.RecordResponse("bot", "ops", 10000) // 10s — exceeds 5s SLA

	violations := m.Check()
	if len(violations) == 0 {
		t.Fatal("expected latency violation")
	}
	if violations[0].Type != "latency" {
		t.Fatalf("expected latency, got %s", violations[0].Type)
	}
	if violations[0].Value != 10000 {
		t.Fatalf("expected value 10000, got %.0f", violations[0].Value)
	}
}

func TestSLACheckNoViolation(t *testing.T) {
	m := New()
	m.AddRule(Rule{
		ID: "r1", Agent: "bot", Tenant: "ops",
		MaxResponseMs: 5000, WindowMinutes: 5, Enabled: true,
	})

	m.RecordResponse("bot", "ops", 2000) // 2s — within SLA

	violations := m.Check()
	if len(violations) != 0 {
		t.Fatalf("expected no violations, got %d", len(violations))
	}
}

func TestSLADisabledRule(t *testing.T) {
	m := New()
	m.AddRule(Rule{
		ID: "r1", Agent: "bot", Tenant: "ops",
		MaxResponseMs: 100, Enabled: false,
	})

	m.RecordResponse("bot", "ops", 50000)
	violations := m.Check()
	if len(violations) != 0 {
		t.Fatal("disabled rule should not trigger")
	}
}

func TestSLAWindowExpiry(t *testing.T) {
	m := New()
	m.AddRule(Rule{
		ID: "r1", Agent: "bot", Tenant: "ops",
		MaxResponseMs: 5000, WindowMinutes: 1, Enabled: true,
	})

	// Record old response outside the 1-minute window
	m.mu.Lock()
	key := "ops/bot"
	m.latencies[key] = append(m.latencies[key], latencyRecord{
		DurationMs: 10000,
		Timestamp:  time.Now().Add(-2 * time.Minute),
	})
	m.mu.Unlock()

	violations := m.Check()
	if len(violations) != 0 {
		t.Fatal("expired data should not trigger violation")
	}
}

func TestSLAErrorRate(t *testing.T) {
	m := New()
	m.AddRule(Rule{
		ID: "r1", Agent: "bot", Tenant: "ops",
		MaxErrorRate: 20, WindowMinutes: 5, Enabled: true,
	})

	// 2 successes, 3 errors = 60% error rate > 20%
	m.RecordResponse("bot", "ops", 1000)
	m.RecordResponse("bot", "ops", 1000)
	m.RecordError("bot", "ops")
	m.RecordError("bot", "ops")
	m.RecordError("bot", "ops")

	violations := m.Check()
	if len(violations) == 0 {
		t.Fatal("expected error_rate violation")
	}
	if violations[0].Type != "error_rate" {
		t.Fatalf("expected error_rate, got %s", violations[0].Type)
	}
}

func TestSLADefaultWindow(t *testing.T) {
	m := New()
	m.AddRule(Rule{
		ID: "r1", Agent: "bot", Tenant: "ops",
		MaxResponseMs: 1000, Enabled: true,
	})

	// Verify default window is 5 minutes
	m.mu.Lock()
	if m.rules[0].WindowMinutes != 5 {
		t.Fatalf("expected default window 5, got %d", m.rules[0].WindowMinutes)
	}
	m.mu.Unlock()
}

func TestSLAAutoID(t *testing.T) {
	m := New()
	m.AddRule(Rule{Agent: "bot", Tenant: "ops", MaxResponseMs: 1000, Enabled: true})

	m.mu.Lock()
	if m.rules[0].ID == "" {
		t.Fatal("expected auto-generated ID")
	}
	m.mu.Unlock()
}

func TestSLAHealthy(t *testing.T) {
	m := New()
	h := m.Health(nil)
	if !h.Healthy {
		t.Fatal("expected healthy")
	}
}
