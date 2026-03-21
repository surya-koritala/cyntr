package tools

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDatabaseToolName(t *testing.T) {
	if NewDatabaseTool().Name() != "database_query" {
		t.Fatal("unexpected name")
	}
}

func TestDatabaseToolMissingParams(t *testing.T) {
	tool := NewDatabaseTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing params")
	}
}

func TestDatabaseToolUnsupportedDriver(t *testing.T) {
	tool := NewDatabaseTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"driver": "mysql", "dsn": "test", "query": "SELECT 1",
	})
	if err == nil {
		t.Fatal("expected error for unsupported driver")
	}
}

func TestDatabaseToolRejectsInsert(t *testing.T) {
	tool := NewDatabaseTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": ":memory:", "query": "INSERT INTO foo VALUES (1)",
	})
	if err == nil {
		t.Fatal("expected error for INSERT")
	}
}

func TestDatabaseToolRejectsDelete(t *testing.T) {
	tool := NewDatabaseTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": ":memory:", "query": "DELETE FROM foo",
	})
	if err == nil {
		t.Fatal("expected error for DELETE")
	}
}

func TestDatabaseToolRejectsDrop(t *testing.T) {
	tool := NewDatabaseTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": ":memory:", "query": "DROP TABLE foo",
	})
	if err == nil {
		t.Fatal("expected error for DROP")
	}
}

func TestDatabaseToolSQLiteSelect(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.Exec("CREATE TABLE users (id INTEGER, name TEXT)")
	db.Exec("INSERT INTO users VALUES (1, 'Alice')")
	db.Exec("INSERT INTO users VALUES (2, 'Bob')")
	db.Close()

	tool := NewDatabaseTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": dbPath, "query": "SELECT id, name FROM users ORDER BY id",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "Alice") {
		t.Fatalf("expected Alice, got %q", result)
	}
	if !strings.Contains(result, "Bob") {
		t.Fatalf("expected Bob, got %q", result)
	}
	if !strings.Contains(result, "| id | name |") {
		t.Fatalf("expected markdown table header, got %q", result)
	}
}

func TestDatabaseToolNoRows(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, _ := sql.Open("sqlite", dbPath)
	db.Exec("CREATE TABLE empty_table (id INTEGER)")
	db.Close()

	tool := NewDatabaseTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": dbPath, "query": "SELECT * FROM empty_table",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result != "Query returned no rows." {
		t.Fatalf("expected no rows message, got %q", result)
	}
}

func TestDatabaseToolNullValues(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, _ := sql.Open("sqlite", dbPath)
	db.Exec("CREATE TABLE t (a TEXT, b TEXT)")
	db.Exec("INSERT INTO t VALUES ('hello', NULL)")
	db.Close()

	tool := NewDatabaseTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": dbPath, "query": "SELECT a, b FROM t",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "NULL") {
		t.Fatalf("expected NULL, got %q", result)
	}
}

func TestDatabaseToolReadOnlyEnforcement(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, _ := sql.Open("sqlite", dbPath)
	db.Exec("CREATE TABLE protect_me (id INTEGER)")
	db.Exec("INSERT INTO protect_me VALUES (1)")
	db.Close()

	// Attempt write via crafted query - should be blocked by keyword check
	tool := NewDatabaseTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": dbPath, "query": "UPDATE protect_me SET id = 2",
	})
	if err == nil {
		t.Fatal("expected error for UPDATE query")
	}
}

func TestDatabaseToolWithCTE(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, _ := sql.Open("sqlite", dbPath)
	db.Exec("CREATE TABLE t (val INTEGER)")
	db.Exec("INSERT INTO t VALUES (10)")
	db.Close()

	tool := NewDatabaseTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"driver": "sqlite", "dsn": dbPath, "query": "WITH cte AS (SELECT val FROM t) SELECT * FROM cte",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(result, "10") {
		t.Fatalf("expected 10, got %q", result)
	}
}

func TestDatabaseToolCleanup(t *testing.T) {
	// Cleanup temp files from other tests
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	os.Remove(dbPath)
}
