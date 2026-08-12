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

func TestMigrateFilePreviewsEnforcesConstraintsAndCascades(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	// SQLite only applies foreign-key actions after this connection-level setting.
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	if err := Migrate(
		db,
		"../../migrations/001_init.sql",
		"../../migrations/002_file_objects.sql",
		"../../migrations/010_file_preview.sql",
	); err != nil {
		t.Fatalf("apply file preview migration: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO file_objects (id, file_hash, storage_path, size, content_type)
		VALUES (1, 'source-hash', 'uploads/source.png', 100, 'image/png')
	`); err != nil {
		t.Fatalf("insert file object: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO file_previews (file_object_id, storage_path, size, content_type, width, height)
		VALUES (1, 'uploads/source-preview.jpg', 50, 'image/jpeg', 320, 240)
	`); err != nil {
		t.Fatalf("insert file preview: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO file_previews (file_object_id, storage_path, size, content_type, width, height)
		VALUES (2, 'uploads/invalid-preview.jpg', 50, 'image/jpeg', 0, 240)
	`); err == nil {
		t.Fatal("expected zero preview width to fail")
	}

	if _, err := db.Exec(`DELETE FROM file_objects WHERE id = 1`); err != nil {
		t.Fatalf("delete file object: %v", err)
	}

	var previewCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM file_previews`).Scan(&previewCount); err != nil {
		t.Fatalf("count previews: %v", err)
	}
	if previewCount != 0 {
		t.Fatalf("preview count = %d, want cascade to remove it", previewCount)
	}
}

func TestMigrateFileScansEnforcesStatusesAndCascades(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	// SQLite only applies foreign-key actions after this connection-level setting.
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	if err := Migrate(
		db,
		"../../migrations/001_init.sql",
		"../../migrations/002_file_objects.sql",
		"../../migrations/011_file_scans.sql",
	); err != nil {
		t.Fatalf("apply file scan migration: %v", err)
	}

	if _, err := db.Exec(`
		INSERT INTO file_objects (id, file_hash, storage_path, size, content_type)
		VALUES (1, 'scan-source-hash', 'uploads/source.bin', 100, 'application/octet-stream')
	`); err != nil {
		t.Fatalf("insert file object: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO file_objects (id, file_hash, storage_path, size, content_type)
		VALUES (2, 'invalid-scan-source-hash', 'uploads/invalid.bin', 100, 'application/octet-stream')
	`); err != nil {
		t.Fatalf("insert second file object: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO file_scans (file_object_id, status) VALUES (1, 'pending')`); err != nil {
		t.Fatalf("insert pending scan: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO file_scans (file_object_id, status) VALUES (2, 'unknown')`); err == nil {
		t.Fatal("expected unsupported scan status to fail")
	}

	if _, err := db.Exec(`DELETE FROM file_objects WHERE id = 1`); err != nil {
		t.Fatalf("delete file object: %v", err)
	}

	var scanCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM file_scans`).Scan(&scanCount); err != nil {
		t.Fatalf("count file scans: %v", err)
	}
	if scanCount != 0 {
		t.Fatalf("scan count = %d, want cascade to remove it", scanCount)
	}
}

func TestMigrateUserAccessCreatesInvitationAndUserControls(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := Migrate(db, "../../migrations/001_init.sql", "../../migrations/012_user_access.sql"); err != nil {
		t.Fatalf("apply access migration: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, role, status, storage_quota_bytes, session_version, must_change_password) VALUES ('admin', 'hash', 'admin', 'active', 1073741824, 1, 0)`); err != nil {
		t.Fatalf("insert controlled user: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO invitations (code_digest, code_hash, created_by_user_id, expires_at) VALUES ('digest', 'hash', 1, CURRENT_TIMESTAMP)`); err != nil {
		t.Fatalf("insert invitation: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (username, password_hash, role) VALUES ('invalid-role', 'hash', 'owner')`); err == nil {
		t.Fatal("expected unsupported role to fail")
	}
}

func TestMigrateCreatesShareAccessAuditConstraints(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := Migrate(db, "../../migrations/013_share_access_audit.sql"); err != nil {
		t.Fatalf("apply share access audit migration: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO share_access_audits (token, ip_hash, action, result) VALUES ('token', 'hashed-ip', 'download', 'allowed')`); err != nil {
		t.Fatalf("insert audit record: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO share_access_audits (token, ip_hash, action, result) VALUES ('token', 'hashed-ip', 'unknown', 'allowed')`); err == nil {
		t.Fatal("expected unsupported audit action to fail")
	}
	if _, err := db.Exec(`INSERT INTO share_access_audits (token, ip_hash, action, result) VALUES ('token', 'hashed-ip', 'download', 'unknown')`); err == nil {
		t.Fatal("expected unsupported audit result to fail")
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
	foldersMigration := "../../migrations/005_folders.sql"
	uploadTaskParentMigration := "../../migrations/006_upload_task_parent.sql"
	fileSharesMigration := "../../migrations/007_file_shares.sql"
	if err := Migrate(db, initMigration, fileObjectsMigration, uploadTasksMigration, fixUploadChunksMigration, foldersMigration, uploadTaskParentMigration, fileSharesMigration); err != nil {
		t.Fatalf("apply upload task, folder, and file share migrations: %v", err)
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

	if _, err := db.Exec(`INSERT INTO folders (user_id, name) VALUES (1, 'documents')`); err != nil {
		t.Fatalf("insert root folder: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO folders (user_id, name) VALUES (1, 'documents')`); err == nil {
		t.Fatal("expected duplicate root folder name to fail")
	}
	if _, err := db.Exec(`
		INSERT INTO upload_tasks (
			id, user_id, parent_id, original_name, content_type,
			file_size, chunk_size, total_chunks, temp_dir
		) VALUES ('upload-in-folder', 1, 1, 'report.pdf', 'application/pdf', 10, 10, 1, 'uploads/tmp/upload-in-folder')
	`); err != nil {
		t.Fatalf("insert upload task with parent folder: %v", err)
	}

	var parentID int64
	if err := db.QueryRow(`SELECT parent_id FROM upload_tasks WHERE id = 'upload-in-folder'`).Scan(&parentID); err != nil {
		t.Fatalf("query upload task parent ID: %v", err)
	}
	if parentID != 1 {
		t.Fatalf("upload task parent ID = %d, want 1", parentID)
	}

	if err := Migrate(db, initMigration, fileObjectsMigration, uploadTasksMigration, fixUploadChunksMigration, foldersMigration, uploadTaskParentMigration, fileSharesMigration); err != nil {
		t.Fatalf("reapply upload task, folder, and file share migrations: %v", err)
	}
}

func TestMigrateFileSharesEnforcesConstraints(t *testing.T) {
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

	if err := Migrate(
		db,
		"../../migrations/001_init.sql",
		"../../migrations/002_file_objects.sql",
		"../../migrations/003_upload_tasks.sql",
		"../../migrations/004_fix_upload_chunks.sql",
		"../../migrations/005_folders.sql",
		"../../migrations/006_upload_task_parent.sql",
		"../../migrations/007_file_shares.sql",
	); err != nil {
		t.Fatalf("apply file share migration: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'sean', 'hash')`); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO file_objects (id, file_hash, storage_path, size, content_type)
		VALUES (1, 'file-hash', 'uploads/file.txt', 15, 'text/plain')
	`); err != nil {
		t.Fatalf("insert file object: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO user_files (id, user_id, original_name, storage_path, size, content_type, object_id)
		VALUES (1, 1, 'file.txt', 'uploads/file.txt', 15, 'text/plain', 1)
	`); err != nil {
		t.Fatalf("insert user file: %v", err)
	}

	// A normal share can reference an existing user file and start with zero downloads.
	if _, err := db.Exec(`
		INSERT INTO file_shares (token, user_file_id, max_downloads)
		VALUES ('share-token', 1, 2)
	`); err != nil {
		t.Fatalf("insert valid share: %v", err)
	}

	var downloadCount int64
	if err := db.QueryRow(`SELECT download_count FROM file_shares WHERE token = 'share-token'`).Scan(&downloadCount); err != nil {
		t.Fatalf("query share: %v", err)
	}
	if downloadCount != 0 {
		t.Fatalf("download count = %d, want 0", downloadCount)
	}

	invalidInserts := []struct {
		name  string
		query string
	}{
		{
			name:  "duplicate token",
			query: `INSERT INTO file_shares (token, user_file_id) VALUES ('share-token', 1)`,
		},
		{
			name:  "empty token",
			query: `INSERT INTO file_shares (token, user_file_id) VALUES ('', 1)`,
		},
		{
			name:  "zero download limit",
			query: `INSERT INTO file_shares (token, user_file_id, max_downloads) VALUES ('zero-limit', 1, 0)`,
		},
		{
			name:  "download count exceeds limit",
			query: `INSERT INTO file_shares (token, user_file_id, max_downloads, download_count) VALUES ('over-limit', 1, 1, 2)`,
		},
		{
			name:  "missing user file",
			query: `INSERT INTO file_shares (token, user_file_id) VALUES ('missing-file', 999)`,
		},
	}

	for _, test := range invalidInserts {
		t.Run(test.name, func(t *testing.T) {
			if _, err := db.Exec(test.query); err == nil {
				t.Fatal("expected insert to fail")
			}
		})
	}

	var indexCount int
	if err := db.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'index'
		  AND name IN ('idx_file_shares_user_file', 'idx_file_shares_expires_at')
	`).Scan(&indexCount); err != nil {
		t.Fatalf("count file share indexes: %v", err)
	}
	if indexCount != 2 {
		t.Fatalf("file share index count = %d, want 2", indexCount)
	}
}
