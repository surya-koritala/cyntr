package tools

import (
	"bytes"
	"compress/flate"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

const (
	pdfMaxFileSize   = 50 * 1024 * 1024 // 50MB
	pdfMaxOutputSize = 64 * 1024         // 64KB
)

type PDFReaderTool struct{}

func NewPDFReaderTool() *PDFReaderTool { return &PDFReaderTool{} }

func (t *PDFReaderTool) Name() string { return "pdf_reader" }
func (t *PDFReaderTool) Description() string {
	return "Extract text from a PDF file. Supports standard text-based PDFs. Returns error for encrypted or image-only PDFs."
}
func (t *PDFReaderTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"file_path": {Type: "string", Description: "Path to the PDF file", Required: true},
	}
}

func (t *PDFReaderTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	filePath := input["file_path"]
	if filePath == "" {
		return "", fmt.Errorf("file_path is required")
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	if info.Size() > pdfMaxFileSize {
		return "", fmt.Errorf("file too large: %d bytes (max %d)", info.Size(), pdfMaxFileSize)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	if len(data) < 5 || string(data[:5]) != "%PDF-" {
		return "", fmt.Errorf("not a valid PDF file")
	}

	// Check for encryption
	if bytes.Contains(data, []byte("/Encrypt")) {
		return "", fmt.Errorf("encrypted PDF: cannot extract text")
	}

	text := extractPDFText(data)
	if text == "" {
		return "", fmt.Errorf("no extractable text found (PDF may be image-only)")
	}

	if len(text) > pdfMaxOutputSize {
		text = text[:pdfMaxOutputSize] + "\n\n[Output truncated at 64KB]"
	}

	return text, nil
}

// extractPDFText extracts text from PDF stream objects using Tj/TJ operators.
func extractPDFText(data []byte) string {
	var allText strings.Builder

	// Find all stream objects and extract text
	streamRe := regexp.MustCompile(`(?s)stream\r?\n(.*?)\r?\nendstream`)
	matches := streamRe.FindAllSubmatch(data, -1)

	for _, match := range matches {
		raw := match[1]
		// Try FlateDecode decompression
		decoded := tryFlateDecompress(raw)
		if decoded == nil {
			decoded = raw
		}
		text := extractTextOperators(decoded)
		if text != "" {
			allText.WriteString(text)
			allText.WriteString("\n")
		}
	}

	return strings.TrimSpace(allText.String())
}

// tryFlateDecompress attempts to decompress flate-encoded data.
func tryFlateDecompress(data []byte) []byte {
	reader := flate.NewReader(bytes.NewReader(data))
	defer reader.Close()
	decoded, err := io.ReadAll(io.LimitReader(reader, pdfMaxOutputSize*2))
	if err != nil {
		return nil
	}
	return decoded
}

// extractTextOperators parses PDF text operators (Tj, TJ, ', ").
func extractTextOperators(data []byte) string {
	var result strings.Builder
	s := string(data)

	// Match Tj operator: (text) Tj
	tjRe := regexp.MustCompile(`\(([^)]*)\)\s*Tj`)
	for _, m := range tjRe.FindAllStringSubmatch(s, -1) {
		result.WriteString(unescapePDFString(m[1]))
	}

	// Match TJ operator: [(text) num (text) ...] TJ
	tjArrayRe := regexp.MustCompile(`\[((?:[^]]*?))\]\s*TJ`)
	tjStrRe := regexp.MustCompile(`\(([^)]*)\)`)
	for _, m := range tjArrayRe.FindAllStringSubmatch(s, -1) {
		for _, s := range tjStrRe.FindAllStringSubmatch(m[1], -1) {
			result.WriteString(unescapePDFString(s[1]))
		}
	}

	// Match ' and " operators (text showing with move)
	quoteRe := regexp.MustCompile(`\(([^)]*)\)\s*'`)
	for _, m := range quoteRe.FindAllStringSubmatch(s, -1) {
		result.WriteString(unescapePDFString(m[1]))
		result.WriteString("\n")
	}

	return result.String()
}

// unescapePDFString handles basic PDF string escape sequences.
func unescapePDFString(s string) string {
	s = strings.ReplaceAll(s, "\\n", "\n")
	s = strings.ReplaceAll(s, "\\r", "\r")
	s = strings.ReplaceAll(s, "\\t", "\t")
	s = strings.ReplaceAll(s, "\\(", "(")
	s = strings.ReplaceAll(s, "\\)", ")")
	s = strings.ReplaceAll(s, "\\\\", "\\")
	return s
}
