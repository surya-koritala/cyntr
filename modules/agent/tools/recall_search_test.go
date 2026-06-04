package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/recall"
)

func TestRecallSearchToolHappyPath(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()

	var got recall.SearchRequest
	bus.Handle("recall", recall.TopicSearch, func(msg ipc.Message) (ipc.Message, error) {
		got = msg.Payload.(recall.SearchRequest)
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: recall.SearchResult{
			Snippets: []recall.Snippet{{Session: "s1", Role: "user", Text: "the q3 budget plan", CreatedAt: time.Now()}},
		}}, nil
	})

	tool := NewRecallSearchTool(bus)
	ctx := agent.WithToolCaller(context.Background(), "acme", "agent1", "jane")
	out, err := tool.Execute(ctx, map[string]string{"query": "budget", "limit": "3"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Scope comes from the caller context, never from tool input.
	if got.Tenant != "acme" || got.User != "jane" {
		t.Fatalf("scope not taken from context: %+v", got)
	}
	if got.Query != "budget" || got.Limit != 3 {
		t.Fatalf("request fields wrong: %+v", got)
	}
	if !strings.Contains(out, "the q3 budget plan") {
		t.Fatalf("output missing snippet text: %q", out)
	}
}

func TestRecallSearchToolValidation(t *testing.T) {
	tool := NewRecallSearchTool(ipc.NewBus())
	ctx := agent.WithToolCaller(context.Background(), "acme", "agent1", "jane")
	if _, err := tool.Execute(ctx, map[string]string{"query": "  "}); err == nil {
		t.Fatal("expected error for empty query")
	}
	if _, err := tool.Execute(context.Background(), map[string]string{"query": "x"}); err == nil {
		t.Fatal("expected error when no tenant/user in context")
	}
}

func TestRecallSearchToolNoModule(t *testing.T) {
	bus := ipc.NewBus() // nothing handles recall.search
	defer bus.Close()
	tool := NewRecallSearchTool(bus)
	ctx := agent.WithToolCaller(context.Background(), "acme", "agent1", "jane")
	out, err := tool.Execute(ctx, map[string]string{"query": "budget"})
	if err != nil {
		t.Fatalf("missing module should degrade gracefully, got %v", err)
	}
	if !strings.Contains(out, "not registered") {
		t.Fatalf("expected graceful message, got %q", out)
	}
}

func TestRecallSearchToolNoResults(t *testing.T) {
	bus := ipc.NewBus()
	defer bus.Close()
	bus.Handle("recall", recall.TopicSearch, func(msg ipc.Message) (ipc.Message, error) {
		return ipc.Message{Type: ipc.MessageTypeResponse, Payload: recall.SearchResult{}}, nil
	})
	tool := NewRecallSearchTool(bus)
	ctx := agent.WithToolCaller(context.Background(), "acme", "agent1", "jane")
	out, _ := tool.Execute(ctx, map[string]string{"query": "nonexistent"})
	if !strings.Contains(out, "No past conversations") {
		t.Fatalf("expected no-results message, got %q", out)
	}
}
