package upload

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

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
		"../../migrations/005_folders.sql",
		"../../migrations/006_upload_task_parent.sql",
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

func sqliteTimestamp(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05")
}

func TestRepositoryCreateAndFindUploadTask(t *testing.T) {
	repo := newTestRepository(t)

	if _, err := repo.db.Exec(`INSERT INTO folders (id, user_id, name) VALUES (1, 1, 'documents')`); err != nil {
		t.Fatalf("create folder: %v", err)
	}
	parentID := int64(1)

	created, err := repo.Create(&Task{
		ID:           "upload-1",
		UserID:       1,
		ParentID:     &parentID,
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
	if created.ParentID == nil || *created.ParentID != parentID {
		t.Fatalf("parent ID = %v, want %d", created.ParentID, parentID)
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
	if found.ParentID == nil || *found.ParentID != parentID {
		t.Fatalf("found parent ID = %v, want %d", found.ParentID, parentID)
	}

	if _, err := repo.FindByID(2, "upload-1"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("other user find error = %v, want %v", err, ErrTaskNotFound)
	}
}

func TestRepositoryUpsertAndListChunks(t *testing.T) {
	repo := newTestRepository(t)

	if _, err := repo.Create(&Task{
		ID:           "upload-1",
		UserID:       1,
		OriginalName: "video.mp4",
		ContentType:  "video/mp4",
		FileSize:     25,
		ChunkSize:    10,
		TotalChunks:  3,
		Status:       StatusUploading,
		TempDir:      "uploads/tmp/upload-1",
	}); err != nil {
		t.Fatalf("create upload task: %v", err)
	}

	first, err := repo.UpsertChunk(&Chunk{
		UploadID: "upload-1",
		Number:   1,
		Size:     10,
		Hash: sql.NullString{
			String: "first-hash",
			Valid:  true,
		},
	})
	if err != nil {
		t.Fatalf("upsert first chunk: %v", err)
	}
	if first.Number != 1 || first.Size != 10 {
		t.Fatalf("first chunk = %#v, want number 1 and size 10", first)
	}

	updated, err := repo.UpsertChunk(&Chunk{
		UploadID: "upload-1",
		Number:   1,
		Size:     8,
		Hash: sql.NullString{
			String: "updated-hash",
			Valid:  true,
		},
	})
	if err != nil {
		t.Fatalf("upsert existing chunk: %v", err)
	}
	if updated.Size != 8 || updated.Hash.String != "updated-hash" {
		t.Fatalf("updated chunk = %#v, want new size and hash", updated)
	}

	if _, err := repo.UpsertChunk(&Chunk{UploadID: "upload-1", Number: 0, Size: 10}); err != nil {
		t.Fatalf("upsert chunk zero: %v", err)
	}

	chunks, err := repo.ListChunks("upload-1")
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(chunks))
	}
	if chunks[0].Number != 0 || chunks[1].Number != 1 {
		t.Fatalf("chunk order = %#v, want 0 then 1", chunks)
	}

	if _, err := repo.FindChunk("upload-1", 2); !errors.Is(err, ErrChunkNotFound) {
		t.Fatalf("missing chunk error = %v, want %v", err, ErrChunkNotFound)
	}
}

func TestRepositoryTransitionStatus(t *testing.T) {
	repo := newTestRepository(t)

	if _, err := repo.Create(&Task{
		ID:           "upload-1",
		UserID:       1,
		OriginalName: "video.mp4",
		ContentType:  "video/mp4",
		FileSize:     10,
		ChunkSize:    10,
		TotalChunks:  1,
		Status:       StatusUploading,
		TempDir:      "uploads/tmp/upload-1",
	}); err != nil {
		t.Fatalf("create upload task: %v", err)
	}

	transitioned, err := repo.TransitionStatus(
		1,
		"upload-1",
		StatusUploading,
		StatusCompleting,
	)
	if err != nil {
		t.Fatalf("transition status: %v", err)
	}
	if !transitioned {
		t.Fatal("expected uploading to completing transition")
	}

	task, err := repo.FindByID(1, "upload-1")
	if err != nil {
		t.Fatalf("find updated task: %v", err)
	}
	if task.Status != StatusCompleting {
		t.Fatalf("status = %q, want %q", task.Status, StatusCompleting)
	}

	transitioned, err = repo.TransitionStatus(
		2,
		"upload-1",
		StatusCompleting,
		StatusCompleted,
	)
	if err != nil {
		t.Fatalf("other user transition: %v", err)
	}
	if transitioned {
		t.Fatal("other user should not transition task status")
	}

	transitioned, err = repo.TransitionStatus(
		1,
		"upload-1",
		StatusUploading,
		StatusCompleted,
	)
	if err != nil {
		t.Fatalf("wrong source status transition: %v", err)
	}
	if transitioned {
		t.Fatal("transition with stale source status should fail")
	}
}

func TestRepositoryTouchesAndListsExpiredUploadingTasks(t *testing.T) {
	repo := newTestRepository(t)

	if _, err := repo.Create(&Task{
		ID:           "expired-upload",
		UserID:       1,
		OriginalName: "expired.mp4",
		ContentType:  "video/mp4",
		FileSize:     10,
		ChunkSize:    10,
		TotalChunks:  1,
		Status:       StatusUploading,
		TempDir:      "uploads/tmp/expired-upload",
	}); err != nil {
		t.Fatalf("create expired task: %v", err)
	}
	if _, err := repo.Create(&Task{
		ID:           "active-upload",
		UserID:       1,
		OriginalName: "active.mp4",
		ContentType:  "video/mp4",
		FileSize:     10,
		ChunkSize:    10,
		TotalChunks:  1,
		Status:       StatusUploading,
		TempDir:      "uploads/tmp/active-upload",
	}); err != nil {
		t.Fatalf("create active task: %v", err)
	}

	if _, err := repo.db.Exec(
		`UPDATE upload_tasks SET updated_at = ? WHERE id = ?`,
		sqliteTimestamp(time.Now().Add(-2*time.Hour)),
		"expired-upload",
	); err != nil {
		t.Fatalf("age upload task: %v", err)
	}

	expired, err := repo.ListExpiredUploading(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("list expired tasks: %v", err)
	}
	if len(expired) != 1 || expired[0].ID != "expired-upload" {
		t.Fatalf("expired tasks = %#v, want expired-upload", expired)
	}

	touched, err := repo.TouchUploading(1, "expired-upload")
	if err != nil {
		t.Fatalf("touch upload task: %v", err)
	}
	if !touched {
		t.Fatal("expected active upload task to be touched")
	}

	expired, err = repo.ListExpiredUploading(time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("list expired tasks after touch: %v", err)
	}
	if len(expired) != 0 {
		t.Fatalf("expired tasks after touch = %#v, want none", expired)
	}
}
