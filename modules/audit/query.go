package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func QueryEntries(db *sql.DB, filter QueryFilter) ([]Entry, error) {
	var conditions []string
	var args []any

	if filter.Tenant != "" {
		conditions = append(conditions, "tenant = ?")
		args = append(args, filter.Tenant)
	}
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
	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}

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
