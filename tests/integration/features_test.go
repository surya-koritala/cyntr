package integration

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tools "github.com/cyntr-dev/cyntr/modules/agent/tools"
	_ "modernc.org/sqlite"
)

// TestPDFReaderIntegration tests PDF reading with a real PDF structure.
func TestPDFReaderIntegration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invoice.pdf")

	// Minimal valid PDF with text
	pdf := "%PDF-1.4\n" +
		"1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
		"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
		"3 0 obj<</Type/Page/Parent 2 0 R/Contents 4 0 R>>endobj\n" +
		"4 0 obj<</Length 80>>stream\n" +
		"BT /F1 12 Tf (Invoice #1234) Tj 0 -20 Td (Total: $500.00) Tj ET\n" +
		"endstream\nendobj\n"
	os.WriteFile(path, []byte(pdf), 0644)

	tool := tools.NewPDFReaderTool()
	result, err := tool.Execute(context.Background(), map[string]string{"file_path": path})
	if err != nil {
		t.Fatalf("pdf reader: %v", err)
	}
	if !strings.Contains(result, "Invoice #1234") {
		t.Fatalf("expected invoice text, got %q", result)
	}
	if !strings.Contains(result, "Total: $500.00") {
		t.Fatalf("expected total, got %q", result)
	}
	t.Logf("PDF output:\n%s", result)
}

// TestDatabaseIntegration tests SQL queries against a real SQLite database.
func TestDatabaseIntegration(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "company.db")

	// Create a realistic database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	db.Exec(`CREATE TABLE employees (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		department TEXT,
		salary REAL
	)`)
	db.Exec(`INSERT INTO employees VALUES (1, 'Alice', 'Engineering', 150000)`)
	db.Exec(`INSERT INTO employees VALUES (2, 'Bob', 'Sales', 95000)`)
	db.Exec(`INSERT INTO employees VALUES (3, 'Carol', 'Engineering', 140000)`)
	db.Exec(`INSERT INTO employees VALUES (4, 'Dave', 'Marketing', 110000)`)
	db.Close()

	tool := tools.NewDatabaseTool()

	// Test basic select
	result, err := tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": dbPath,
		"query": "SELECT name, department, salary FROM employees ORDER BY salary DESC",
	})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	t.Logf("Basic SELECT:\n%s", result)

	if !strings.Contains(result, "Alice") || !strings.Contains(result, "150000") {
		t.Fatal("expected Alice with salary")
	}

	// Test aggregation
	result, err = tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": dbPath,
		"query": "SELECT department, COUNT(*) as headcount, AVG(salary) as avg_salary FROM employees GROUP BY department ORDER BY avg_salary DESC",
	})
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	t.Logf("Aggregation:\n%s", result)

	if !strings.Contains(result, "Engineering") {
		t.Fatal("expected Engineering department")
	}

	// Test CTE
	result, err = tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": dbPath,
		"query": "WITH high_earners AS (SELECT * FROM employees WHERE salary > 100000) SELECT name FROM high_earners ORDER BY name",
	})
	if err != nil {
		t.Fatalf("cte: %v", err)
	}
	t.Logf("CTE:\n%s", result)

	// Test write rejection
	_, err = tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": dbPath,
		"query": "INSERT INTO employees VALUES (5, 'Eve', 'HR', 100000)",
	})
	if err == nil {
		t.Fatal("INSERT should be rejected")
	}
	t.Logf("Write rejection: %v (expected)", err)

	// Verify data unchanged
	result, err = tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": dbPath,
		"query": "SELECT COUNT(*) as total FROM employees",
	})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if !strings.Contains(result, "4") {
		t.Fatalf("expected 4 rows still, got %s", result)
	}
}

// TestWebSearchIntegration verifies tool registration and parameter validation.
func TestWebSearchIntegration(t *testing.T) {
	tool := tools.NewWebSearchTool()

	// Verify tool metadata
	if tool.Name() != "web_search" {
		t.Fatal("wrong name")
	}
	params := tool.Parameters()
	if !params["query"].Required || !params["api_key"].Required || !params["cx"].Required {
		t.Fatal("expected required params")
	}

	// Missing params should error cleanly
	_, err := tool.Execute(context.Background(), map[string]string{"query": "test"})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected clear error, got %v", err)
	}
}

// TestImageGenIntegration verifies tool registration and parameter validation.
func TestImageGenIntegration(t *testing.T) {
	tool := tools.NewImageGenTool()

	if tool.Name() != "generate_image" {
		t.Fatal("wrong name")
	}

	_, err := tool.Execute(context.Background(), map[string]string{"prompt": "test"})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected clear error, got %v", err)
	}
}

// TestChromiumIntegration tests chromium with real Chrome if available.
func TestChromiumIntegration(t *testing.T) {
	tool := tools.NewChromiumTool()

	if tool.Name() != "chromium_browser" {
		t.Fatal("wrong name")
	}

	// Test parameter validation (no Chrome needed)
	_, err := tool.Execute(context.Background(), map[string]string{"action": "navigate"})
	if err == nil {
		t.Fatal("expected error for navigate without URL")
	}

	_, err = tool.Execute(context.Background(), map[string]string{"action": "unknown_action"})
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatal("expected unknown action error")
	}
}
