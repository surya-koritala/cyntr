package observability

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Instrument names — kept as constants so tests / dashboards can reference them.
const (
	MetricChatRequests   = "cyntr.agent.chat.requests"
	MetricToolCalls      = "cyntr.tool.calls"
	MetricToolDuration   = "cyntr.tool.duration_ms"
	MetricChatDuration   = "cyntr.agent.chat.duration_ms"
	MetricLLMTokensTotal = "cyntr.llm.tokens.total"
)

// instruments bundles the canonical Cyntr OTel instruments. They are created
// lazily the first time someone asks for them via Instruments() and cached on
// the package level so subsequent calls are cheap.
type instruments struct {
	chatRequests   metric.Int64Counter
	toolCalls      metric.Int64Counter
	toolDuration   metric.Float64Histogram
	chatDuration   metric.Float64Histogram
	llmTokensTotal metric.Int64Counter
}

var (
	instOnce sync.Once
	instSet  *instruments
	instErr  error
)

// initInstruments forces creation of the canonical instruments now (rather
// than at first use) so they show up in Prometheus scrapes from boot.
func initInstruments() error {
	_ = Instruments() // populates instSet
	return instErr
}

// Instruments returns the lazily-created canonical instrument set. It will
// always return a non-nil pointer; if instrument creation fails (unlikely
// with the no-op MeterProvider), the embedded instruments are nil and the
// Record* helpers below short-circuit.
func Instruments() *instruments {
	instOnce.Do(func() {
		m := otel.Meter("github.com/cyntr-dev/cyntr/modules/observability")
		s := &instruments{}
		var err error

		if s.chatRequests, err = m.Int64Counter(
			MetricChatRequests,
			metric.WithDescription("Count of agent chat requests"),
		); err != nil {
			instErr = err
		}
		if s.toolCalls, err = m.Int64Counter(
			MetricToolCalls,
			metric.WithDescription("Count of tool invocations"),
		); err != nil {
			instErr = err
		}
		if s.toolDuration, err = m.Float64Histogram(
			MetricToolDuration,
			metric.WithDescription("Tool execution duration"),
			metric.WithUnit("ms"),
		); err != nil {
			instErr = err
		}
		if s.chatDuration, err = m.Float64Histogram(
			MetricChatDuration,
			metric.WithDescription("Agent chat round-trip duration"),
			metric.WithUnit("ms"),
		); err != nil {
			instErr = err
		}
		if s.llmTokensTotal, err = m.Int64Counter(
			MetricLLMTokensTotal,
			metric.WithDescription("Total LLM tokens used (input + output)"),
		); err != nil {
			instErr = err
		}

		instSet = s
	})
	return instSet
}

// RecordChatRequest increments the chat-request counter. status is typically
// "ok", "error", or "rate_limited".
func RecordChatRequest(ctx context.Context, tenant, agent, status string) {
	i := Instruments()
	if i == nil || i.chatRequests == nil {
		return
	}
	i.chatRequests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tenant", tenant),
		attribute.String("agent", agent),
		attribute.String("status", status),
	))
}

// RecordChatDuration records the wall-clock duration of an agent chat in ms.
func RecordChatDuration(ctx context.Context, tenant, agent string, durationMs float64) {
	i := Instruments()
	if i == nil || i.chatDuration == nil {
		return
	}
	i.chatDuration.Record(ctx, durationMs, metric.WithAttributes(
		attribute.String("tenant", tenant),
		attribute.String("agent", agent),
	))
}

// RecordToolCall bumps the tool-calls counter. status is "ok" | "denied" | "error".
func RecordToolCall(ctx context.Context, tenant, agent, tool, status string) {
	i := Instruments()
	if i == nil || i.toolCalls == nil {
		return
	}
	i.toolCalls.Add(ctx, 1, metric.WithAttributes(
		attribute.String("tenant", tenant),
		attribute.String("agent", agent),
		attribute.String("tool", tool),
		attribute.String("status", status),
	))
}

// RecordToolDuration records a tool execution duration in ms.
func RecordToolDuration(ctx context.Context, tool string, durationMs float64) {
	i := Instruments()
	if i == nil || i.toolDuration == nil {
		return
	}
	i.toolDuration.Record(ctx, durationMs, metric.WithAttributes(
		attribute.String("tool", tool),
	))
}

// RecordLLMTokens adds to the running token counter. kind is "input" | "output".
func RecordLLMTokens(ctx context.Context, tenant, provider, kind string, tokens int64) {
	if tokens <= 0 {
		return
	}
	i := Instruments()
	if i == nil || i.llmTokensTotal == nil {
		return
	}
	i.llmTokensTotal.Add(ctx, tokens, metric.WithAttributes(
		attribute.String("tenant", tenant),
		attribute.String("provider", provider),
		attribute.String("kind", kind),
	))
}
