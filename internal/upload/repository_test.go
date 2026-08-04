package upload

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/SeanidHau/CloudBox/internal/database"
)

func newTestRepository(t *testing.T) *Repository {
	t.Helper()

	db, err := database.Open(filepath.Join(t.TempDir(), "cloudbox-test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := database.Migrate(
		db,
		"../../migrations/001_init.sql",
		"../../migrations/002_file_objects.sql",
		"../../migrations/003_upload_tasks.sql",
		"../../migrations/004_fix_upload_chunks.sql",
	); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'user-1', 'hash-1')`); err != nil {
		t.Fatalf("insert user 1: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (2, 'user-2', 'hash-2')`); err != nil {
		t.Fatalf("insert user 2: %v", err)
	}

	return NewRepository(db)
}

func TestRepositoryCreateAndFindUploadTask(t *testing.T) {
	repo := newTestRepository(t)

	created, err := repo.Create(&Task{
		ID:           "upload-1",
		UserID:       1,
		OriginalName: "video.mp4",
		ContentType:  "video/mp4",
		FileSize:     25,
		ChunkSize:    10,
		TotalChunks:  3,
		FileHash: sql.NullString{
			String: "file-hash",
			Valid:  true,
		},
		Status:  StatusUploading,
		TempDir: "uploads/tmp/upload-1",
	})
	if err != nil {
		t.Fatalf("create upload task: %v", err)
	}
	if created.ID != "upload-1" {
		t.Fatalf("task ID = %q, want %q", created.ID, "upload-1")
	}
	if created.TotalChunks != 3 {
		t.Fatalf("total chunks = %d, want 3", created.TotalChunks)
	}
	if !created.FileHash.Valid || created.FileHash.String != "file-hash" {
		t.Fatalf("file hash = %#v, want file-hash", created.FileHash)
	}
	if created.Status != StatusUploading {
		t.Fatalf("status = %q, want %q", created.Status, StatusUploading)
	}

	found, err := repo.FindByID(1, "upload-1")
	if err != nil {
		t.Fatalf("find upload task: %v", err)
	}
	if found.TempDir != "uploads/tmp/upload-1" {
		t.Fatalf("temp dir = %q, want %q", found.TempDir, "uploads/tmp/upload-1")
	}

	if _, err := repo.FindByID(2, "upload-1"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("other user find error = %v, want %v", err, ErrTaskNotFound)
	}
}
