package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/modules/agent"
	"github.com/cyntr-dev/cyntr/modules/recall"
)

// RecallSearchTool lets an agent search its own past conversations across
// sessions. The (tenant, user) scope is taken from the tool-call context, so
// the agent can only ever recall the calling user's own history.
type RecallSearchTool struct {
	bus *ipc.Bus
}

// NewRecallSearchTool constructs a RecallSearchTool talking to the recall
// module over bus.
func NewRecallSearchTool(bus *ipc.Bus) *RecallSearchTool {
	return &RecallSearchTool{bus: bus}
}

func (t *RecallSearchTool) Name() string { return "recall_search" }

func (t *RecallSearchTool) Description() string {
	return "Search your own past conversations with this user across previous sessions. Use this to recall earlier discussions, decisions, or facts the user mentioned before. Returns the most relevant past messages, best match first."
}

func (t *RecallSearchTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"query": {Type: "string", Description: "What to look for in past conversations", Required: true},
		"limit": {Type: "string", Description: "Max results to return (default 5)", Required: false},
	}
}

func (t *RecallSearchTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	query := strings.TrimSpace(input["query"])
	if query == "" {
		return "", fmt.Errorf("recall_search: query is required")
	}
	tenant, _, user := agent.ToolCaller(ctx)
	if tenant == "" || user == "" {
		return "", fmt.Errorf("recall_search: no tenant/user in tool context")
	}
	if t.bus == nil {
		return "", fmt.Errorf("recall_search: bus not configured")
	}

	limit := 5
	if v := strings.TrimSpace(input["limit"]); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	resp, err := t.bus.Request(callCtx, ipc.Message{
		Source: "recall_search", Target: "recall", Topic: recall.TopicSearch,
		Payload: recall.SearchRequest{Tenant: tenant, User: user, Query: query, Limit: limit},
	})
	if err != nil {
		if err == ipc.ErrNoHandler {
			return "(recall module not registered)", nil
		}
		return "", fmt.Errorf("recall_search: %w", err)
	}
	result, ok := resp.Payload.(recall.SearchResult)
	if !ok {
		return "", fmt.Errorf("recall_search: unexpected payload %T", resp.Payload)
	}
	return formatSnippets(query, result.Snippets), nil
}

func formatSnippets(query string, snippets []recall.Snippet) string {
	if len(snippets) == 0 {
		return fmt.Sprintf("No past conversations found matching %q.", query)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Found %d past message(s) matching %q:\n\n", len(snippets), query)
	for i, s := range snippets {
		when := ""
		if !s.CreatedAt.IsZero() {
			when = " (" + s.CreatedAt.UTC().Format("2006-01-02") + ")"
		}
		role := s.Role
		if role == "" {
			role = "message"
		}
		fmt.Fprintf(&b, "%d. [%s%s] %s\n", i+1, role, when, s.Text)
	}
	return b.String()
}
