package tools

import (
	"context"
	"encoding/csv"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

type CSVQueryTool struct{}

func NewCSVQueryTool() *CSVQueryTool { return &CSVQueryTool{} }

func (t *CSVQueryTool) Name() string { return "csv_query" }
func (t *CSVQueryTool) Description() string {
	return "Parse and analyze CSV data. Get headers, row count, column statistics, filter rows, and sort. Handles CSV without shell piping."
}
func (t *CSVQueryTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"csv_data": {Type: "string", Description: "CSV data as a string (with headers)", Required: true},
		"action":   {Type: "string", Description: "Action: headers, count, stats, filter, sort, select", Required: true},
		"column":   {Type: "string", Description: "Column name (for stats, filter, sort, select)", Required: false},
		"value":    {Type: "string", Description: "Filter value or sort direction (asc/desc)", Required: false},
	}
}

func (t *CSVQueryTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	csvData := input["csv_data"]
	action := input["action"]
	if csvData == "" || action == "" {
		return "", fmt.Errorf("csv_data and action are required")
	}

	reader := csv.NewReader(strings.NewReader(csvData))
	records, err := reader.ReadAll()
	if err != nil {
		return "", fmt.Errorf("parse CSV: %w", err)
	}
	if len(records) < 1 {
		return "", fmt.Errorf("empty CSV")
	}

	headers := records[0]
	rows := records[1:]

	switch action {
	case "headers":
		return "Columns: " + strings.Join(headers, ", ") + fmt.Sprintf("\nRows: %d", len(rows)), nil

	case "count":
		return fmt.Sprintf("%d rows", len(rows)), nil

	case "stats":
		col := input["column"]
		colIdx := findColumn(headers, col)
		if colIdx < 0 {
			return "", fmt.Errorf("column %q not found (available: %s)", col, strings.Join(headers, ", "))
		}
		var values []float64
		for _, row := range rows {
			if colIdx < len(row) {
				if v, err := strconv.ParseFloat(strings.TrimSpace(row[colIdx]), 64); err == nil {
					values = append(values, v)
				}
			}
		}
		if len(values) == 0 {
			return fmt.Sprintf("Column %q: no numeric values found", col), nil
		}
		sort.Float64s(values)
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		mean := sum / float64(len(values))
		variance := 0.0
		for _, v := range values {
			variance += (v - mean) * (v - mean)
		}
		stddev := math.Sqrt(variance / float64(len(values)))

		return fmt.Sprintf("Column: %s\nCount: %d\nMin: %.2f\nMax: %.2f\nMean: %.2f\nStdDev: %.2f\nMedian: %.2f",
			col, len(values), values[0], values[len(values)-1], mean, stddev, values[len(values)/2]), nil

	case "filter":
		col := input["column"]
		val := input["value"]
		colIdx := findColumn(headers, col)
		if colIdx < 0 {
			return "", fmt.Errorf("column %q not found", col)
		}
		var filtered [][]string
		for _, row := range rows {
			if colIdx < len(row) && strings.Contains(strings.ToLower(row[colIdx]), strings.ToLower(val)) {
				filtered = append(filtered, row)
			}
		}
		return formatCSVResult(headers, filtered), nil

	case "sort":
		col := input["column"]
		direction := input["value"]
		if direction == "" {
			direction = "asc"
		}
		colIdx := findColumn(headers, col)
		if colIdx < 0 {
			return "", fmt.Errorf("column %q not found", col)
		}
		sorted := make([][]string, len(rows))
		copy(sorted, rows)
		sort.Slice(sorted, func(i, j int) bool {
			a, b := "", ""
			if colIdx < len(sorted[i]) {
				a = sorted[i][colIdx]
			}
			if colIdx < len(sorted[j]) {
				b = sorted[j][colIdx]
			}
			if direction == "desc" {
				return a > b
			}
			return a < b
		})
		return formatCSVResult(headers, sorted), nil

	case "select":
		col := input["column"]
		colIdx := findColumn(headers, col)
		if colIdx < 0 {
			return "", fmt.Errorf("column %q not found", col)
		}
		var values []string
		for _, row := range rows {
			if colIdx < len(row) {
				values = append(values, row[colIdx])
			}
		}
		return strings.Join(values, "\n"), nil

	default:
		return "", fmt.Errorf("unknown action %q: use headers, count, stats, filter, sort, select", action)
	}
}

func findColumn(headers []string, name string) int {
	for i, h := range headers {
		if strings.EqualFold(strings.TrimSpace(h), strings.TrimSpace(name)) {
			return i
		}
	}
	return -1
}

func formatCSVResult(headers []string, rows [][]string) string {
	if len(rows) == 0 {
		return "No matching rows."
	}
	var sb strings.Builder
	sb.WriteString("| " + strings.Join(headers, " | ") + " |\n")
	sb.WriteString("|" + strings.Repeat(" --- |", len(headers)) + "\n")
	for _, row := range rows {
		sb.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	sb.WriteString(fmt.Sprintf("\n%d rows", len(rows)))
	return sb.String()
}
