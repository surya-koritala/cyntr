package agent

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestRunMigrations(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Run migrations
	if err := RunMigrations(db); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	// Check migrations were recorded
	var count int
	db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count)
	if count != len(Migrations) {
		t.Fatalf("expected %d migrations recorded, got %d", len(Migrations), count)
	}
}

func TestRunMigrationsIdempotent(t *testing.T) {
	db, _ := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	defer db.Close()

	// Run twice — should not error
	RunMigrations(db)
	if err := RunMigrations(db); err != nil {
		t.Fatalf("second run should be idempotent: %v", err)
	}
}

func TestMigrationsHaveDescriptions(t *testing.T) {
	for _, m := range Migrations {
		if m.Description == "" {
			t.Fatalf("migration v%d missing description", m.Version)
		}
		if m.SQL == "" {
			t.Fatalf("migration v%d missing SQL", m.Version)
		}
	}
}
