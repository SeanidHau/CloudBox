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

	migrationPath := filepath.Join("..", "..", "migrations", "postgres", "001_init.sql")
	if err := Migrate(db, migrationPath); err != nil {
		t.Fatalf("apply Postgres baseline migration: %v", err)
	}
	if err := Migrate(db, migrationPath); err != nil {
		t.Fatalf("reapply Postgres baseline migration: %v", err)
	}

	var tableCount int
	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN (
			  'users', 'file_objects', 'folders', 'user_files',
			  'upload_tasks', 'upload_chunks', 'file_shares'
		  )
	`).Scan(&tableCount)
	if err != nil {
		t.Fatalf("count baseline tables: %v", err)
	}
	if tableCount != 7 {
		t.Fatalf("baseline table count = %d, want 7", tableCount)
	}

	var migrationCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM schema_migrations WHERE name = $1`,
		filepath.Base(migrationPath),
	).Scan(&migrationCount); err != nil {
		t.Fatalf("count migration records: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("migration count = %d, want 1", migrationCount)
	}
}
