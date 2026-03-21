package tools

import (
	"context"
	"strings"
	"testing"
)

func TestJSONQueryToolName(t *testing.T) {
	if NewJSONQueryTool().Name() != "json_query" {
		t.Fatal("wrong name")
	}
}

func TestJSONQuerySimpleKey(t *testing.T) {
	tool := NewJSONQueryTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"json_data": `{"name":"Alice","age":30}`,
		"path":      "name",
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result != "Alice" {
		t.Fatalf("expected Alice, got %q", result)
	}
}

func TestJSONQueryNestedPath(t *testing.T) {
	tool := NewJSONQueryTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"json_data": `{"data":{"user":{"email":"a@b.com"}}}`,
		"path":      "data.user.email",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "a@b.com" {
		t.Fatalf("expected email, got %q", result)
	}
}

func TestJSONQueryArrayIndex(t *testing.T) {
	tool := NewJSONQueryTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"json_data": `{"items":[{"id":1},{"id":2},{"id":3}]}`,
		"path":      "items[1].id",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "2" {
		t.Fatalf("expected 2, got %q", result)
	}
}

func TestJSONQueryLength(t *testing.T) {
	tool := NewJSONQueryTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"json_data": `{"items":["a","b","c"]}`,
		"path":      "items.length",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "3" {
		t.Fatalf("expected 3, got %q", result)
	}
}

func TestJSONQueryPrettyPrint(t *testing.T) {
	tool := NewJSONQueryTool()
	result, err := tool.Execute(context.Background(), map[string]string{
		"json_data": `{"a":1,"b":2}`,
		"path":      "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "\"a\": 1") {
		t.Fatalf("expected pretty JSON, got %q", result)
	}
}

func TestJSONQueryInvalidJSON(t *testing.T) {
	tool := NewJSONQueryTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"json_data": "not json",
		"path":      "x",
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestJSONQueryMissingKey(t *testing.T) {
	tool := NewJSONQueryTool()
	_, err := tool.Execute(context.Background(), map[string]string{
		"json_data": `{"a":1}`,
		"path":      "b",
	})
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestJSONQueryBoolean(t *testing.T) {
	tool := NewJSONQueryTool()
	result, _ := tool.Execute(context.Background(), map[string]string{
		"json_data": `{"active":true}`,
		"path":      "active",
	})
	if result != "true" {
		t.Fatalf("expected true, got %q", result)
	}
}

func TestJSONQueryMissingData(t *testing.T) {
	tool := NewJSONQueryTool()
	_, err := tool.Execute(context.Background(), map[string]string{})
	if err == nil {
		t.Fatal("expected error for missing json_data")
	}
}
