package tools

import (
	"context"
	"strings"
	"testing"
)

const testCSV = "name,age,city\nAlice,30,NYC\nBob,25,LA\nCarol,35,NYC\nDave,28,Chicago"

func TestCSVQueryToolName(t *testing.T) {
	if NewCSVQueryTool().Name() != "csv_query" {
		t.Fatal("wrong name")
	}
}

func TestCSVQueryHeaders(t *testing.T) {
	tool := NewCSVQueryTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"csv_data": testCSV, "action": "headers",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "name") || !strings.Contains(result, "age") || !strings.Contains(result, "city") {
		t.Fatalf("expected headers, got %q", result)
	}
	if !strings.Contains(result, "4") {
		t.Fatalf("expected row count 4, got %q", result)
	}
}

func TestCSVQueryCount(t *testing.T) {
	tool := NewCSVQueryTool()
	result, _ := tool.Execute(context.Background(), map[string]string{
		"csv_data": testCSV, "action": "count",
	})
	if result != "4 rows" {
		t.Fatalf("expected 4 rows, got %q", result)
	}
}

func TestCSVQueryStats(t *testing.T) {
	tool := NewCSVQueryTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"csv_data": testCSV, "action": "stats", "column": "age",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Min: 25") || !strings.Contains(result, "Max: 35") {
		t.Fatalf("expected stats, got %q", result)
	}
}

func TestCSVQueryFilter(t *testing.T) {
	tool := NewCSVQueryTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"csv_data": testCSV, "action": "filter", "column": "city", "value": "NYC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Alice") || !strings.Contains(result, "Carol") {
		t.Fatalf("expected NYC results, got %q", result)
	}
	if strings.Contains(result, "Bob") {
		t.Fatal("Bob should not be in NYC filter")
	}
}

func TestCSVQuerySort(t *testing.T) {
	tool := NewCSVQueryTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"csv_data": testCSV, "action": "sort", "column": "age", "value": "desc",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Carol (35) should be first
	lines := strings.Split(result, "\n")
	if len(lines) > 2 && !strings.Contains(lines[2], "Carol") {
		t.Fatalf("expected Carol first in desc sort, got %q", lines[2])
	}
}

func TestCSVQuerySelect(t *testing.T) {
	tool := NewCSVQueryTool()
	result, _ := tool.Execute(context.Background(), map[string]string{
		"csv_data": testCSV, "action": "select", "column": "name",
	})
	if !strings.Contains(result, "Alice") || !strings.Contains(result, "Bob") {
		t.Fatalf("expected names, got %q", result)
	}
}

func TestCSVQueryMissingColumn(t *testing.T) {
	tool := NewCSVQueryTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"csv_data": testCSV, "action": "stats", "column": "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for missing column")
	}
}

func TestCSVQueryInvalidAction(t *testing.T) {
	tool := NewCSVQueryTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"csv_data": testCSV, "action": "dance",
	})
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
}

func TestCSVQueryMissingParams(t *testing.T) {
	tool := NewCSVQueryTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing params")
	}
}
