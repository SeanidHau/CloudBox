//go:build integration

package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigratePostgresBaseline(t *testing.T) {
	databaseURL := os.Getenv("POSTGRES_INTEGRATION_URL")
	if databaseURL == "" {
		t.Skip("set POSTGRES_INTEGRATION_URL to run the PostgreSQL integration test")
	}

	db, err := OpenPostgres(databaseURL)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	migrationPaths := []string{
		filepath.Join("..", "..", "migrations", "postgres", "001_init.sql"),
		filepath.Join("..", "..", "migrations", "postgres", "002_background_jobs.sql"),
		filepath.Join("..", "..", "migrations", "postgres", "003_background_job_user.sql"),
		filepath.Join("..", "..", "migrations", "postgres", "004_file_preview.sql"),
	}
	if err := Migrate(db, migrationPaths...); err != nil {
		t.Fatalf("apply Postgres migrations: %v", err)
	}
	if err := Migrate(db, migrationPaths...); err != nil {
		t.Fatalf("reapply Postgres migrations: %v", err)
	}

	var tableCount int
	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN (
			  'users', 'file_objects', 'folders', 'user_files',
			  'upload_tasks', 'upload_chunks', 'file_shares',
			  'background_jobs', 'file_previews'
		  )
	`).Scan(&tableCount)
	if err != nil {
		t.Fatalf("count migrated tables: %v", err)
	}
	if tableCount != 9 {
		t.Fatalf("migrated table count = %d, want 9", tableCount)
	}

	for _, migrationPath := range migrationPaths {
		var migrationCount int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM schema_migrations WHERE name = $1`,
			filepath.Base(migrationPath),
		).Scan(&migrationCount); err != nil {
			t.Fatalf("count migration record for %s: %v", migrationPath, err)
		}
		if migrationCount != 1 {
			t.Fatalf("migration count for %s = %d, want 1", migrationPath, migrationCount)
		}
	}
}
