package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// defaultQueryLimit caps an unspecified (Limit <= 0) audit query so a
	// caller cannot pull an entire tenant's history in one request.
	defaultQueryLimit = 1000
	// maxQueryLimit is the hard upper bound; any larger request is clamped to
	// protect the process from unbounded result materialization (DoS).
	maxQueryLimit = 10000
)

func QueryEntries(db *sql.DB, filter QueryFilter) ([]Entry, error) {
	// Tenant scoping is mandatory: an empty tenant must never widen the result
	// set to every tenant's audit log.
	if filter.Tenant == "" {
		return nil, fmt.Errorf("audit query: tenant is required")
	}
	var conditions []string
	var args []any

	conditions = append(conditions, "tenant = ?")
	args = append(args, filter.Tenant)
	if filter.ActionType != "" {
		conditions = append(conditions, "json_extract(data, '$.action.type') = ?")
		args = append(args, filter.ActionType)
	}
	if filter.User != "" {
		conditions = append(conditions, "json_extract(data, '$.principal.user') = ?")
		args = append(args, filter.User)
	}
	if filter.Agent != "" {
		conditions = append(conditions, "json_extract(data, '$.principal.agent') = ?")
		args = append(args, filter.Agent)
	}
	if !filter.Since.IsZero() {
		conditions = append(conditions, "timestamp >= ?")
		args = append(args, filter.Since.UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	if !filter.Until.IsZero() {
		conditions = append(conditions, "timestamp <= ?")
		args = append(args, filter.Until.UTC().Format("2006-01-02T15:04:05.000Z"))
	}

	query := "SELECT data FROM audit_entries"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY rowid ASC"
	// Always bound the result set. A non-positive Limit means "unspecified",
	// which we treat as the default rather than unlimited; anything above the
	// hard cap is clamped down.
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultQueryLimit
	}
	if limit > maxQueryLimit {
		limit = maxQueryLimit
	}
	query += fmt.Sprintf(" LIMIT %d", limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		var entry Entry
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			return nil, fmt.Errorf("unmarshal entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if entries == nil {
		entries = []Entry{}
	}
	return entries, rows.Err()
}
