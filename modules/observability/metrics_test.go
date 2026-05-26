package observability

import (
	"context"
	"testing"
)

// TestInstruments_NoopSafe ensures all Record* helpers are safe to call when
// the module is in no-op mode (i.e. no provider configured). They must not
// panic and must not error — instrumentation sites call them unconditionally.
func TestInstruments_NoopSafe(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	ctx := context.Background()

	// First call triggers lazy init; subsequent calls hit the cached set.
	for i := 0; i < 2; i++ {
		RecordChatRequest(ctx, "tenantA", "agent1", "ok")
		RecordChatDuration(ctx, "tenantA", "agent1", 12.5)
		RecordToolCall(ctx, "tenantA", "agent1", "shell", "ok")
		RecordToolDuration(ctx, "shell", 3.14)
		RecordLLMTokens(ctx, "tenantA", "anthropic", "input", 100)
		RecordLLMTokens(ctx, "tenantA", "anthropic", "output", 200)
	}

	// Zero / negative token counts should be a no-op (and not panic).
	RecordLLMTokens(ctx, "tenantA", "anthropic", "input", 0)
	RecordLLMTokens(ctx, "tenantA", "anthropic", "input", -1)
}

// TestInstruments_Registration verifies the lazy instrument set is non-nil
// and all five canonical instruments wired up.
func TestInstruments_Registration(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	// Reset the sync.Once so this test is independent of ordering with the
	// no-op test above. We can't reset directly, but Instruments() returns
	// the same pointer regardless of order — assert that all 5 instruments
	// are populated.
	i := Instruments()
	if i == nil {
		t.Fatal("Instruments() returned nil")
	}
	if i.chatRequests == nil {
		t.Error("chatRequests counter not registered")
	}
	if i.toolCalls == nil {
		t.Error("toolCalls counter not registered")
	}
	if i.toolDuration == nil {
		t.Error("toolDuration histogram not registered")
	}
	if i.chatDuration == nil {
		t.Error("chatDuration histogram not registered")
	}
	if i.llmTokensTotal == nil {
		t.Error("llmTokensTotal counter not registered")
	}
}
