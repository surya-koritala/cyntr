package agent

import (
	"database/sql"
	"fmt"

	"github.com/cyntr-dev/cyntr/kernel/log"
)

var migrationLogger = log.Default().WithModule("migrations")

// Migration represents a single database schema change.
type Migration struct {
	Version     int
	Description string
	SQL         string
}

// Migrations is the ordered list of schema changes.
var Migrations = []Migration{
	{
		Version:     1,
		Description: "Add users table",
		SQL:         `CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, tenant TEXT NOT NULL, name TEXT NOT NULL, email TEXT DEFAULT '', role TEXT DEFAULT 'user', api_key_hash TEXT DEFAULT '', created_at TEXT NOT NULL)`,
	},
	{
		Version:     2,
		Description: "Add messages FTS index",
		SQL:         `CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(content)`,
	},
	{
		Version:     3,
		Description: "Add usage tracking table",
		SQL: `CREATE TABLE IF NOT EXISTS usage (
			id TEXT PRIMARY KEY, timestamp TEXT NOT NULL, tenant TEXT NOT NULL,
			agent TEXT NOT NULL, provider TEXT NOT NULL,
			input_tokens INTEGER DEFAULT 0, output_tokens INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0, duration_ms INTEGER DEFAULT 0
		)`,
	},
	{
		Version:     4,
		Description: "Add agent_versions table",
		SQL: `CREATE TABLE IF NOT EXISTS agent_versions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			tenant TEXT NOT NULL,
			name TEXT NOT NULL,
			version INTEGER NOT NULL,
			config TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	},
}

// RunMigrations executes pending migrations on the given database.
func RunMigrations(db *sql.DB) error {
	// Create migrations tracking table
	db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		description TEXT,
		applied_at TEXT
	)`)

	for _, m := range Migrations {
		// Check if already applied
		var count int
		db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", m.Version).Scan(&count)
		if count > 0 {
			continue
		}

		// Apply migration
		if _, err := db.Exec(m.SQL); err != nil {
			return fmt.Errorf("migration v%d (%s) failed: %w", m.Version, m.Description, err)
		}

		// Record migration
		db.Exec("INSERT INTO schema_migrations (version, description, applied_at) VALUES (?, ?, datetime('now'))",
			m.Version, m.Description)

		migrationLogger.Info("migration applied", map[string]any{"version": m.Version, "description": m.Description})
	}

	return nil
}
