package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type scriptedProvider struct {
	name  string
	err   error
	resp  string
	calls *int
}

func (p *scriptedProvider) Name() string { return p.name }
func (p *scriptedProvider) Chat(ctx context.Context, _ []Message, _ []ToolDef) (Message, error) {
	if p.calls != nil {
		*p.calls++
	}
	if p.err != nil {
		return Message{}, p.err
	}
	return Message{Role: RoleAssistant, Content: p.resp}, nil
}

func TestFailoverAdvancesOnTransient(t *testing.T) {
	var c2 int
	primary := &scriptedProvider{name: "p1", err: errors.New("OpenAI error 429: rate limit")}
	secondary := &scriptedProvider{name: "p2", resp: "from secondary", calls: &c2}

	var attempts []string
	f := NewFailoverProvider("chain", primary, secondary)
	f.SetOnAttempt(func(prov string, err error) { attempts = append(attempts, prov) })

	msg, err := f.Chat(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if msg.Content != "from secondary" {
		t.Fatalf("expected secondary's response, got %q", msg.Content)
	}
	if c2 != 1 {
		t.Fatalf("secondary should have been called once, got %d", c2)
	}
	if len(attempts) != 2 || attempts[0] != "p1" || attempts[1] != "p2" {
		t.Fatalf("metrics hook saw %v, want [p1 p2]", attempts)
	}
}

func TestFailoverAllFailAggregate(t *testing.T) {
	f := NewFailoverProvider("chain",
		&scriptedProvider{name: "p1", err: errors.New("error 429 rate limit")},
		&scriptedProvider{name: "p2", err: errors.New("error 503 server error")},
	)
	_, err := f.Chat(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("all-fail should error")
	}
	if !strings.Contains(err.Error(), "p1") || !strings.Contains(err.Error(), "p2") {
		t.Fatalf("aggregate error should mention both providers: %v", err)
	}
}

func TestFailoverNonTransientStopsEarly(t *testing.T) {
	var c2 int
	f := NewFailoverProvider("chain",
		&scriptedProvider{name: "p1", err: errors.New("error 400: invalid request")},
		&scriptedProvider{name: "p2", resp: "should not run", calls: &c2},
	)
	if _, err := f.Chat(context.Background(), nil, nil); err == nil {
		t.Fatal("non-transient error should propagate")
	}
	if c2 != 0 {
		t.Fatal("a 400 must fail fast — the rest of the chain should not run")
	}
}

func TestFailoverKeyRotation(t *testing.T) {
	// Same provider type, two keys: first key is unauthorized, second works.
	var c2 int
	f := NewFailoverProvider("openai-rotated",
		&scriptedProvider{name: "openai#keyA", err: errors.New("OpenAI error 401: unauthorized")},
		&scriptedProvider{name: "openai#keyB", resp: "ok", calls: &c2},
	)
	msg, err := f.Chat(context.Background(), nil, nil)
	if err != nil || msg.Content != "ok" {
		t.Fatalf("auth failure should rotate to the next key: %v / %q", err, msg.Content)
	}
	if c2 != 1 {
		t.Fatal("second key should have been tried")
	}
}

func TestFailoverFirstSuccessWins(t *testing.T) {
	var c2 int
	f := NewFailoverProvider("chain",
		&scriptedProvider{name: "p1", resp: "primary"},
		&scriptedProvider{name: "p2", resp: "secondary", calls: &c2},
	)
	msg, _ := f.Chat(context.Background(), nil, nil)
	if msg.Content != "primary" {
		t.Fatalf("primary success should win, got %q", msg.Content)
	}
	if c2 != 0 {
		t.Fatal("secondary should not be called when primary succeeds")
	}
}

func TestIsTransientClassification(t *testing.T) {
	transient := []string{"error 429", "rate limit hit", "503 server error", "request timeout", "401 unauthorized", "connection refused"}
	for _, s := range transient {
		if !isTransientProviderError(errors.New(s)) {
			t.Fatalf("%q should be transient", s)
		}
	}
	permanent := []string{"error 400: invalid request", "bad request: missing field"}
	for _, s := range permanent {
		if isTransientProviderError(errors.New(s)) {
			t.Fatalf("%q should be permanent", s)
		}
	}
	if isTransientProviderError(context.DeadlineExceeded) != true {
		t.Fatal("deadline exceeded should be transient")
	}
}
