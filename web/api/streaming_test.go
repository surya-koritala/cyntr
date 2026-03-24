package api

import (
	"strings"
	"testing"
)

func TestSplitIntoStreamChunksShort(t *testing.T) {
	chunks := splitIntoStreamChunks("Hello world")
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk for short text, got %d", len(chunks))
	}
	if chunks[0] != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", chunks[0])
	}
}

func TestSplitIntoStreamChunksSentences(t *testing.T) {
	text := "This is the first sentence that has enough content to be meaningful. This is the second sentence with additional details. And here is a third one that rounds it out nicely."
	chunks := splitIntoStreamChunks(text)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for sentences, got %d", len(chunks))
	}
	// Reassemble should equal original
	reassembled := strings.Join(chunks, "")
	if reassembled != text {
		t.Fatalf("reassembled text doesn't match:\n  got: %q\n  want: %q", reassembled, text)
	}
}

func TestSplitIntoStreamChunksNewlines(t *testing.T) {
	text := "This is the first line with enough content to exceed the minimum threshold for splitting.\nThis is the second line with additional details and context.\nAnd a third line to complete the set."
	chunks := splitIntoStreamChunks(text)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for newlines, got %d", len(chunks))
	}
}

func TestSplitIntoStreamChunksLongNoBreak(t *testing.T) {
	// 300 chars with no sentence boundaries
	text := strings.Repeat("abcdefghij", 30)
	chunks := splitIntoStreamChunks(text)
	if len(chunks) < 2 {
		t.Fatalf("expected hard-split for long text without breaks, got %d chunks", len(chunks))
	}
	for _, c := range chunks {
		if len(c) > 200 {
			t.Fatalf("chunk too large: %d chars", len(c))
		}
	}
}

func TestSplitIntoStreamChunksEmpty(t *testing.T) {
	chunks := splitIntoStreamChunks("")
	if len(chunks) != 1 || chunks[0] != "" {
		t.Fatalf("expected single empty chunk, got %v", chunks)
	}
}

func TestSplitIntoStreamChunksPreservesContent(t *testing.T) {
	text := "Hello! How are you? I'm doing well. Let me help you with that.\nHere's the answer."
	chunks := splitIntoStreamChunks(text)
	reassembled := strings.Join(chunks, "")
	if reassembled != text {
		t.Fatal("content not preserved after splitting")
	}
}
