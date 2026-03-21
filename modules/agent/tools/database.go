package tools

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/agent"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

const (
	dbMaxRows       = 100
	dbMaxOutputSize = 64 * 1024
)

type DatabaseTool struct{}

func NewDatabaseTool() *DatabaseTool { return &DatabaseTool{} }

func (t *DatabaseTool) Name() string { return "database_query" }
func (t *DatabaseTool) Description() string {
	return "Execute a read-only SQL query against SQLite or PostgreSQL. Returns results as a markdown table. SELECT queries only."
}
func (t *DatabaseTool) Parameters() map[string]agent.ToolParam {
	return map[string]agent.ToolParam{
		"driver": {Type: "string", Description: "Database driver: sqlite or postgres", Required: true},
		"dsn":    {Type: "string", Description: "Data source name / connection string", Required: true},
		"query":  {Type: "string", Description: "SQL SELECT query to execute", Required: true},
	}
}

func (t *DatabaseTool) Execute(ctx context.Context, input map[string]string) (string, error) {
	driver := input["driver"]
	dsn := input["dsn"]
	query := input["query"]

	if driver == "" || dsn == "" || query == "" {
		return "", fmt.Errorf("driver, dsn, and query are required")
	}

	if driver != "sqlite" && driver != "postgres" {
		return "", fmt.Errorf("unsupported driver %q: use sqlite or postgres", driver)
	}

	// Reject non-SELECT queries at the keyword level
	normalized := strings.TrimSpace(strings.ToUpper(query))
	if !strings.HasPrefix(normalized, "SELECT") && !strings.HasPrefix(normalized, "WITH") {
		return "", fmt.Errorf("only SELECT queries are allowed")
	}
	for _, kw := range []string{"INSERT ", "UPDATE ", "DELETE ", "DROP ", "ALTER ", "CREATE ", "TRUNCATE ", "EXEC "} {
		if strings.Contains(normalized, kw) {
			return "", fmt.Errorf("query contains forbidden keyword: %s", strings.TrimSpace(kw))
		}
	}
	// Block multiple statements
	if strings.Contains(query, ";") && strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";")) != strings.TrimSpace(query[:strings.Index(query, ";")]) {
		// Only allow trailing semicolons, not multiple statements
		parts := strings.Split(strings.TrimSpace(query), ";")
		nonEmpty := 0
		for _, p := range parts {
			if strings.TrimSpace(p) != "" {
				nonEmpty++
			}
		}
		if nonEmpty > 1 {
			return "", fmt.Errorf("multiple statements are not allowed")
		}
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return "", fmt.Errorf("connect: %w", err)
	}
	defer db.Close()

	// Enforce read-only at the database level
	if driver == "sqlite" {
		db.Exec("PRAGMA query_only = ON")
	}

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return "", fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("columns: %w", err)
	}

	// Build markdown table
	var sb strings.Builder
	sb.WriteString("| " + strings.Join(cols, " | ") + " |\n")
	sb.WriteString("|" + strings.Repeat(" --- |", len(cols)) + "\n")

	rowCount := 0
	for rows.Next() {
		if rowCount >= dbMaxRows {
			sb.WriteString(fmt.Sprintf("\n[Truncated: showing %d of more rows]", dbMaxRows))
			break
		}

		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "", fmt.Errorf("scan row: %w", err)
		}

		var cells []string
		for _, v := range values {
			if v == nil {
				cells = append(cells, "NULL")
			} else {
				cells = append(cells, fmt.Sprintf("%v", v))
			}
		}
		sb.WriteString("| " + strings.Join(cells, " | ") + " |\n")
		rowCount++

		if sb.Len() > dbMaxOutputSize {
			sb.WriteString("\n[Output truncated at 64KB]")
			break
		}
	}

	if rowCount == 0 {
		return "Query returned no rows.", nil
	}

	return sb.String(), nil
}
