package tools

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/cyntr-dev/cyntr/kernel/netguard"
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

// validatePostgresHost extracts the host(s) from a PostgreSQL DSN (either
// URL form "postgres://host:port/db" or keyword form "host=... port=...") and
// rejects the connection unless every host resolves only to public addresses.
// It reuses the shared netguard so the loopback / link-local (cloud metadata) /
// private / ULA / multicast blocklist cannot drift from other callers.
func validatePostgresHost(dsn string) error {
	if netguard.AllowPrivate() {
		return nil
	}

	hosts := postgresHosts(dsn)
	if len(hosts) == 0 {
		// No explicit host means libpq would connect to localhost / a unix
		// socket. Fail closed.
		return fmt.Errorf("ssrf guard: postgres DSN must specify an explicit host")
	}

	for _, host := range hosts {
		host = strings.TrimSpace(host)
		if host == "" {
			return fmt.Errorf("ssrf guard: postgres DSN must specify an explicit host")
		}
		// A path implies a unix-domain socket (local) — reject.
		if strings.HasPrefix(host, "/") {
			return fmt.Errorf("ssrf guard: unix-socket postgres connections are not allowed")
		}
		if ip := net.ParseIP(host); ip != nil {
			if !netguard.IsPublicIP(ip) {
				return fmt.Errorf("ssrf guard: postgres host resolves to non-public address %s", ip)
			}
			continue
		}
		ips, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("ssrf guard: cannot resolve postgres host %q", host)
		}
		for _, ip := range ips {
			if !netguard.IsPublicIP(ip) {
				return fmt.Errorf("ssrf guard: postgres host resolves to non-public address %s", ip)
			}
		}
	}
	return nil
}

// postgresHosts returns the host(s) named in a PostgreSQL DSN, supporting both
// the URL form and the space-separated keyword form. libpq allows a
// comma-separated list of hosts; each is returned.
func postgresHosts(dsn string) []string {
	trimmed := strings.TrimSpace(dsn)
	if strings.HasPrefix(trimmed, "postgres://") || strings.HasPrefix(trimmed, "postgresql://") {
		u, err := url.Parse(trimmed)
		if err != nil {
			return nil
		}
		// url.Hostname strips the port; a multi-host list lands in Host.
		hostPart := u.Host
		if at := strings.LastIndex(hostPart, "@"); at >= 0 {
			hostPart = hostPart[at+1:]
		}
		var out []string
		for _, hp := range strings.Split(hostPart, ",") {
			h := hp
			if idx := strings.LastIndex(h, ":"); idx >= 0 && !strings.Contains(h[idx+1:], "]") {
				h = h[:idx]
			}
			out = append(out, strings.Trim(h, "[]"))
		}
		return out
	}

	// Keyword form: host=... possibly quoted.
	for _, field := range strings.Fields(trimmed) {
		kv := strings.SplitN(field, "=", 2)
		if len(kv) == 2 && kv[0] == "host" {
			val := strings.Trim(kv[1], "'\"")
			return strings.Split(val, ",")
		}
	}
	return nil
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

	// SSRF guard: a postgres DSN names a network host. Without validation the
	// model could point database_query at an internal/metadata address. Reject
	// any host that does not resolve to a public, routable IP.
	if driver == "postgres" {
		if err := validatePostgresHost(dsn); err != nil {
			return "", err
		}
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
