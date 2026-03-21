package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type JSONQueryTool struct{}

func NewJSONQueryTool() *JSONQueryTool { return &JSONQueryTool{} }

func (t *JSONQueryTool) Name() string { return "json_query" }
func (t *JSONQueryTool) Description() string {
	return "Parse JSON data and extract values using dot-notation paths. Supports nested objects and arrays. Example path: 'items[0].name' or 'data.users.length'"
}
func (t *JSONQueryTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"json_data": {Type: "string", Description: "JSON string to parse", Required: true},
		"path":      {Type: "string", Description: "Dot-notation path to extract (e.g., 'name', 'items[0].id', 'data.count'). Use empty string to return formatted JSON.", Required: false},
	}
}

func (t *JSONQueryTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	jsonData := input["json_data"]
	if jsonData == "" {
		return "", fmt.Errorf("json_data is required")
	}

	var data any
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}

	path := input["path"]
	if path == "" {
		// Pretty print
		pretty, _ := json.MarshalIndent(data, "", "  ")
		return string(pretty), nil
	}

	result, err := queryPath(data, path)
	if err != nil {
		return "", err
	}

	switch v := result.(type) {
	case string:
		return v, nil
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), nil
		}
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(v), nil
	case nil:
		return "null", nil
	default:
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b), nil
	}
}

func queryPath(data any, path string) (any, error) {
	parts := splitPath(path)
	current := data

	for _, part := range parts {
		if part == "length" || part == "len" {
			switch v := current.(type) {
			case []any:
				return float64(len(v)), nil
			case map[string]any:
				return float64(len(v)), nil
			case string:
				return float64(len(v)), nil
			default:
				return nil, fmt.Errorf("cannot get length of %T", current)
			}
		}

		// Check for array index: name[0]
		if idx := strings.Index(part, "["); idx >= 0 {
			key := part[:idx]
			idxStr := strings.TrimSuffix(part[idx+1:], "]")
			index, err := strconv.Atoi(idxStr)
			if err != nil {
				return nil, fmt.Errorf("invalid array index: %s", part)
			}

			if key != "" {
				m, ok := current.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("expected object at %q, got %T", key, current)
				}
				current = m[key]
			}

			arr, ok := current.([]any)
			if !ok {
				return nil, fmt.Errorf("expected array at %q, got %T", part, current)
			}
			if index < 0 || index >= len(arr) {
				return nil, fmt.Errorf("index %d out of range (length %d)", index, len(arr))
			}
			current = arr[index]
			continue
		}

		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected object at %q, got %T", part, current)
		}
		val, exists := m[part]
		if !exists {
			return nil, fmt.Errorf("key %q not found", part)
		}
		current = val
	}

	return current, nil
}

func splitPath(path string) []string {
	var parts []string
	current := ""
	for _, c := range path {
		if c == '.' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
