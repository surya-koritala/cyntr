// Package recall gives an agent memory of its own past conversations.
//
// It maintains a full-text index over every message an agent exchanges (fed by
// the agent.turn_completed event) and, per session, a rolling LLM-written
// summary. Agents query it mid-turn via the recall_search tool; operators and
// other modules query it over the recall.search IPC topic.
//
// Everything is strictly tenant- and user-scoped: a search only ever returns
// rows belonging to the calling (tenant, user) pair.
package recall

import (
	"context"
	"time"
)

// IPC topic served by the module.
const TopicSearch = "recall.search"

// JobKindSummarize is the kernel/jobs kind used to (re)summarize a session.
const JobKindSummarize = "recall.summarize"

// Snippet is one matching past message.
type Snippet struct {
	Session   string    `json:"session"`
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	Score     float64   `json:"score"` // bm25 relevance; lower is better
	CreatedAt time.Time `json:"created_at"`
}

// SearchRequest is the recall.search IPC payload.
type SearchRequest struct {
	Tenant string
	User   string
	Query  string
	Limit  int
}

// SearchResult is the recall.search IPC response.
type SearchResult struct {
	Snippets []Snippet
}

// IndexedMessage is one message to add to the recall index.
type IndexedMessage struct {
	MsgID     string
	Tenant    string
	User      string
	Session   string
	Role      string
	Text      string
	CreatedAt time.Time
}

// SummarizeFunc turns a rendered conversation transcript into a short summary.
// It is wired to an LLM provider in main.go; when nil, summarization is
// skipped and search still works.
type SummarizeFunc func(ctx context.Context, conversation string) (string, error)
