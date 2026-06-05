package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// FailoverProvider is a ModelProvider that wraps an ordered chain of providers
// and advances to the next one when a call fails with a transient error
// (rate-limit, server error, timeout, or auth failure). It is itself a
// ModelProvider, so an agent can target it by name like any other model.
//
// Auth-profile / key rotation is expressed as multiple entries of the same
// provider type with different keys in the chain — e.g. [openai-keyA,
// openai-keyB, anthropic]: a 401/429 on keyA rotates to keyB before falling
// through to a different provider entirely.
type FailoverProvider struct {
	name      string
	chain     []ModelProvider
	transient func(error) bool
	// onAttempt is called after every provider attempt (success has err==nil),
	// for metrics. Optional.
	onAttempt func(provider string, err error)
}

// NewFailoverProvider builds a failover provider named `name` over chain.
func NewFailoverProvider(name string, chain ...ModelProvider) *FailoverProvider {
	return &FailoverProvider{name: name, chain: chain, transient: isTransientProviderError}
}

// SetOnAttempt wires a per-attempt metrics callback.
func (f *FailoverProvider) SetOnAttempt(fn func(provider string, err error)) { f.onAttempt = fn }

func (f *FailoverProvider) Name() string { return f.name }

// Chat tries each provider in order. A transient failure advances to the next;
// a non-transient failure (e.g. a 400 bad request) fails fast since another
// provider won't fix it. If every provider fails, an aggregate error is
// returned.
func (f *FailoverProvider) Chat(ctx context.Context, messages []Message, tools []ToolDef) (Message, error) {
	if len(f.chain) == 0 {
		return Message{}, errors.New("failover: empty provider chain")
	}
	var errs []error
	for _, p := range f.chain {
		msg, err := p.Chat(ctx, messages, tools)
		if f.onAttempt != nil {
			f.onAttempt(p.Name(), err)
		}
		if err == nil {
			return msg, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", p.Name(), err))
		if !f.transient(err) {
			// Permanent error — don't waste the rest of the chain.
			return Message{}, fmt.Errorf("failover: non-retryable error: %w", err)
		}
	}
	return Message{}, fmt.Errorf("failover: all %d providers failed: %v", len(f.chain), errs)
}

// isTransientProviderError reports whether err is worth retrying on the next
// provider/key: rate limits, server errors, timeouts, transport failures, and
// auth failures (which rotate to the next key). Client errors like 400 are
// treated as permanent.
func isTransientProviderError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	s := strings.ToLower(err.Error())
	// Permanent client errors first.
	if strings.Contains(s, "400") || strings.Contains(s, "bad request") || strings.Contains(s, "invalid request") {
		return false
	}
	for _, sig := range []string{
		"429", "rate limit", "rate-limit", "too many requests",
		"500", "502", "503", "504", "server error", "overloaded",
		"timeout", "deadline", "connection refused", "eof", "no such host", "temporarily",
		"401", "403", "unauthorized", "forbidden", // rotate to next key/profile
	} {
		if strings.Contains(s, sig) {
			return true
		}
	}
	return false
}
