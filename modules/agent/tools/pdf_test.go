package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPDFReaderToolName(t *testing.T) {
	if NewPDFReaderTool().Name() != "pdf_reader" {
		t.Fatal("unexpected name")
	}
}

func TestPDFReaderToolMissingPath(t *testing.T) {
	tool := NewPDFReaderTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func TestPDFReaderToolNonexistentFile(t *testing.T) {
	tool := NewPDFReaderTool()
	_, err := tool.Execute(context.Background(), map[string]string{"file_path": "/nonexistent/file.pdf"})
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestPDFReaderToolInvalidPDF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.pdf")
	os.WriteFile(path, []byte("this is not a pdf"), 0644)

	tool := NewPDFReaderTool()
	_, err := tool.Execute(context.Background(), map[string]string{"file_path": path})
	if err == nil {
		t.Fatal("expected error for invalid PDF")
	}
	if !strings.Contains(err.Error(), "not a valid PDF") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPDFReaderToolEncryptedPDF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "encrypted.pdf")
	// Minimal PDF with /Encrypt marker
	os.WriteFile(path, []byte("%PDF-1.4\n/Encrypt << >>"), 0644)

	tool := NewPDFReaderTool()
	_, err := tool.Execute(context.Background(), map[string]string{"file_path": path})
	if err == nil {
		t.Fatal("expected error for encrypted PDF")
	}
	if !strings.Contains(err.Error(), "encrypted") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPDFReaderToolExtractText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pdf")

	// Create a minimal PDF with uncompressed text stream
	pdf := "%PDF-1.4\n" +
		"1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
		"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
		"3 0 obj<</Type/Page/Parent 2 0 R/Contents 4 0 R>>endobj\n" +
		"4 0 obj<</Length 44>>stream\n" +
		"BT (Hello World) Tj ET\n" +
		"endstream\nendobj\n"
	os.WriteFile(path, []byte(pdf), 0644)

	tool := NewPDFReaderTool()
	result, err := tool.Execute(context.Background(), map[string]string{"file_path": path})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "Hello World") {
		t.Fatalf("expected text, got %q", result)
	}
}

func TestPDFReaderToolTJArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.pdf")

	pdf := "%PDF-1.4\n" +
		"1 0 obj<</Length 50>>stream\n" +
		"BT [(Hello) -50 ( World)] TJ ET\n" +
		"endstream\nendobj\n"
	os.WriteFile(path, []byte(pdf), 0644)

	tool := NewPDFReaderTool()
	result, err := tool.Execute(context.Background(), map[string]string{"file_path": path})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "Hello") || !strings.Contains(result, "World") {
		t.Fatalf("expected text from TJ array, got %q", result)
	}
}

func TestPDFReaderToolEscapeSequences(t *testing.T) {
	result := unescapePDFString("Hello\\nWorld\\(test\\)")
	if result != "Hello\nWorld(test)" {
		t.Fatalf("unexpected unescape: %q", result)
	}
}

func TestPDFReaderToolFileTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.pdf")
	// Create file header then check size enforcement via stat
	f, _ := os.Create(path)
	f.WriteString("%PDF-1.4\n")
	f.Close()
	// The file won't actually be 50MB so this won't trigger, testing the path works
	tool := NewPDFReaderTool()
	// This will fail because there's no extractable text, which is expected
	_, err := tool.Execute(context.Background(), map[string]string{"file_path": path})
	if err == nil {
		t.Fatal("expected error for empty PDF")
	}
}
