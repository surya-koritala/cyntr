package recall

import (
	"context"
	"fmt"
	"strings"
)

// Summarizer (re)builds the rolling summary for a session from its recent
// messages using an injected SummarizeFunc.
type Summarizer struct {
	store       *Store
	fn          SummarizeFunc
	maxMessages int
}

// NewSummarizer builds a Summarizer. maxMessages bounds how many of a
// session's most recent messages are fed to the model (default 50).
func NewSummarizer(store *Store, fn SummarizeFunc, maxMessages int) *Summarizer {
	if maxMessages <= 0 {
		maxMessages = 50
	}
	return &Summarizer{store: store, fn: fn, maxMessages: maxMessages}
}

// SummarizeSession renders the session transcript, summarizes it, and stores
// the result keyed by the current message count. It is a no-op when the
// session has no messages or no SummarizeFunc is configured. Safe to run more
// than once (idempotent) — a retry simply recomputes the summary.
func (s *Summarizer) SummarizeSession(ctx context.Context, tenant, user, session string) error {
	if s.fn == nil {
		return nil
	}
	msgs, total, err := s.store.SessionMessages(tenant, user, session, s.maxMessages)
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}

	var b strings.Builder
	for _, m := range msgs {
		role := m.Role
		if role == "" {
			role = "message"
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(m.Text)
		b.WriteString("\n")
	}

	summary, err := s.fn(ctx, b.String())
	if err != nil {
		return fmt.Errorf("recall: summarize: %w", err)
	}
	return s.store.UpsertSummary(tenant, user, session, strings.TrimSpace(summary), total)
}
