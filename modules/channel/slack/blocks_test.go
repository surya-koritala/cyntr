package slack

import "testing"

func TestFormatAsBlocksEmpty(t *testing.T) {
	blocks := FormatAsBlocks("")
	if blocks != nil {
		t.Fatal("expected nil for empty text")
	}
}

func TestFormatAsBlocksPlainText(t *testing.T) {
	blocks := FormatAsBlocks("Hello world")
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	section := blocks[0]
	if section["type"] != "section" {
		t.Fatalf("expected section type, got %v", section["type"])
	}
}

func TestFormatAsBlocksCodeBlock(t *testing.T) {
	text := "Before code\n```\nvar x = 1;\n```\nAfter code"
	blocks := FormatAsBlocks(text)
	if len(blocks) < 2 {
		t.Fatalf("expected multiple blocks for code, got %d", len(blocks))
	}
}

func TestFormatAsBlocksMultipleSections(t *testing.T) {
	text := "Section 1\n```\ncode\n```\nSection 2"
	blocks := FormatAsBlocks(text)
	if len(blocks) < 3 {
		t.Fatalf("expected 3+ blocks, got %d", len(blocks))
	}
}
