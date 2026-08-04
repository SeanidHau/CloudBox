package database

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateAppliesMigrationOnlyOnce(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	migrationPath := filepath.Join(t.TempDir(), "001_create_test_records.sql")
	migrationSQL := `
CREATE TABLE test_records (
    id INTEGER PRIMARY KEY,
    value TEXT NOT NULL
);
INSERT INTO test_records (id, value) VALUES (1, 'created once');
`
	if err := os.WriteFile(migrationPath, []byte(migrationSQL), 0600); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	if err := Migrate(db, migrationPath); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := Migrate(db, migrationPath); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var recordCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM test_records`).Scan(&recordCount); err != nil {
		t.Fatalf("count test records: %v", err)
	}
	if recordCount != 1 {
		t.Fatalf("record count = %d, want 1", recordCount)
	}

	var migrationCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, filepath.Base(migrationPath)).Scan(&migrationCount); err != nil {
		t.Fatalf("count migration records: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("migration count = %d, want 1", migrationCount)
	}
}

func TestMigrateBackfillsLegacyFilesIntoFileObjects(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	initMigration := "../../migrations/001_init.sql"
	fileObjectsMigration := "../../migrations/002_file_objects.sql"
	if err := Migrate(db, initMigration); err != nil {
		t.Fatalf("apply init migration: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'sean', 'hash')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO user_files (user_id, original_name, storage_path, size, content_type, status)
		VALUES (1, 'legacy.txt', 'uploads/legacy.txt', 15, 'text/plain', 'active')
	`); err != nil {
		t.Fatalf("insert legacy file: %v", err)
	}

	if err := Migrate(db, initMigration, fileObjectsMigration); err != nil {
		t.Fatalf("apply file objects migration: %v", err)
	}

	var (
		objectID       int64
		fileHash       string
		storagePath    string
		size           int64
		referenceCount int
	)
	err = db.QueryRow(`
		SELECT fo.id, fo.file_hash, fo.storage_path, fo.size, fo.reference_count
		FROM user_files AS uf
		JOIN file_objects AS fo ON fo.id = uf.object_id
		WHERE uf.id = 1
	`).Scan(&objectID, &fileHash, &storagePath, &size, &referenceCount)
	if err != nil {
		t.Fatalf("query backfilled object: %v", err)
	}
	if objectID == 0 {
		t.Fatal("expected legacy file to have an object ID")
	}
	if fileHash != "legacy-1" {
		t.Fatalf("file hash = %q, want %q", fileHash, "legacy-1")
	}
	if storagePath != "uploads/legacy.txt" {
		t.Fatalf("storage path = %q, want %q", storagePath, "uploads/legacy.txt")
	}
	if size != 15 {
		t.Fatalf("size = %d, want %d", size, 15)
	}
	if referenceCount != 1 {
		t.Fatalf("reference count = %d, want %d", referenceCount, 1)
	}

	if err := Migrate(db, initMigration, fileObjectsMigration); err != nil {
		t.Fatalf("reapply migrations: %v", err)
	}

	var objectCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM file_objects`).Scan(&objectCount); err != nil {
		t.Fatalf("count file objects: %v", err)
	}
	if objectCount != 1 {
		t.Fatalf("file object count = %d, want 1", objectCount)
	}
}

func TestMigrateCreatesUploadTasksAndChunks(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	initMigration := "../../migrations/001_init.sql"
	fileObjectsMigration := "../../migrations/002_file_objects.sql"
	uploadTasksMigration := "../../migrations/003_upload_tasks.sql"
	fixUploadChunksMigration := "../../migrations/004_fix_upload_chunks.sql"
	if err := Migrate(db, initMigration, fileObjectsMigration, uploadTasksMigration, fixUploadChunksMigration); err != nil {
		t.Fatalf("apply upload task migrations: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'sean', 'hash')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO upload_tasks (
			id, user_id, original_name, content_type,
			file_size, chunk_size, total_chunks, temp_dir
		) VALUES ('upload-1', 1, 'video.mp4', 'video/mp4', 25, 10, 3, 'uploads/tmp/upload-1')
	`); err != nil {
		t.Fatalf("insert upload task: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO upload_chunks (upload_id, chunk_number, size, chunk_hash)
		VALUES ('upload-1', 0, 10, 'chunk-hash')
	`); err != nil {
		t.Fatalf("insert upload chunk: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO upload_chunks (upload_id, chunk_number, size)
		VALUES ('upload-1', 0, 10)
	`); err == nil {
		t.Fatal("expected duplicate chunk number to fail")
	}

	if _, err := db.Exec(`
		INSERT INTO upload_chunks (upload_id, chunk_number, size)
		VALUES ('missing-upload', 1, 10)
	`); err == nil {
		t.Fatal("expected missing upload task to fail")
	}

	if err := Migrate(db, initMigration, fileObjectsMigration, uploadTasksMigration, fixUploadChunksMigration); err != nil {
		t.Fatalf("reapply upload task migrations: %v", err)
	}
}
