package slack

import "testing"

func TestChunkMessageShort(t *testing.T) {
	chunks := chunkMessage("hello", 100)
	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Fatalf("expected single chunk, got %v", chunks)
	}
}

func TestChunkMessageExactLimit(t *testing.T) {
	text := "abcde"
	chunks := chunkMessage(text, 5)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestChunkMessageParagraphSplit(t *testing.T) {
	text := "paragraph one\n\nparagraph two\n\nparagraph three"
	chunks := chunkMessage(text, 25)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d: %v", len(chunks), chunks)
	}
}

func TestChunkMessageLineSplit(t *testing.T) {
	text := "line one\nline two\nline three\nline four"
	chunks := chunkMessage(text, 20)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}

func TestChunkMessageHardCut(t *testing.T) {
	text := "abcdefghijklmnopqrstuvwxyz"
	chunks := chunkMessage(text, 10)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}

func TestChunkMessageIndicators(t *testing.T) {
	text := "aaaa\n\nbbbb\n\ncccc"
	chunks := chunkMessage(text, 8)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	// First chunk should have indicator
	if len(chunks[0]) < 5 || chunks[0][:1] != "[" {
		t.Fatalf("expected chunk indicator, got %q", chunks[0])
	}
}
