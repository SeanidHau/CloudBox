package file

import (
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

func TestRepositoryCreateListSoftDeleteAndRestore(t *testing.T) {
	repo := newTestRepository(t)

	created, err := repo.Create(1, "test.txt", "uploads/test.txt", 15, "text/plain")
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if created.Status != StatusActive {
		t.Fatalf("status = %q, want %q", created.Status, StatusActive)
	}

	active, err := repo.ListActive(1)
	if err != nil {
		t.Fatalf("list active files: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("active file count = %d, want 1", len(active))
	}

	if _, err := repo.FindActiveByID(2, created.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("other user find error = %v, want %v", err, ErrFileNotFound)
	}

	if err := repo.SoftDelete(1, created.ID); err != nil {
		t.Fatalf("soft delete file: %v", err)
	}

	active, err = repo.ListActive(1)
	if err != nil {
		t.Fatalf("list active files after delete: %v", err)
	}
	if len(active) != 0 {
		t.Fatalf("active file count after delete = %d, want 0", len(active))
	}

	deleted, err := repo.ListDeleted(1)
	if err != nil {
		t.Fatalf("list deleted files: %v", err)
	}
	if len(deleted) != 1 {
		t.Fatalf("deleted file count = %d, want 1", len(deleted))
	}
	if !deleted[0].DeletedAt.Valid {
		t.Fatal("expected deleted_at to be set")
	}

	if err := repo.Restore(1, created.ID); err != nil {
		t.Fatalf("restore file: %v", err)
	}

	restored, err := repo.FindActiveByID(1, created.ID)
	if err != nil {
		t.Fatalf("find restored file: %v", err)
	}
	if restored.Status != StatusActive {
		t.Fatalf("restored status = %q, want %q", restored.Status, StatusActive)
	}
}

func TestRepositoryCreateAndFindFileObject(t *testing.T) {
	repo := newTestRepository(t)

	created, err := repo.CreateFileObject(
		"a9c2a8c997d2a80c4756e14b6c80e7a5ed8f0262ba1e430ac0c0e751ea0b3abe",
		"uploads/object.txt",
		15,
		"text/plain",
	)
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected file object ID")
	}
	if created.ReferenceCount != 0 {
		t.Fatalf("reference count = %d, want 0", created.ReferenceCount)
	}

	found, err := repo.FindFileObjectByHash(created.FileHash)
	if err != nil {
		t.Fatalf("find file object: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("found object ID = %d, want %d", found.ID, created.ID)
	}
	if found.StoragePath != "uploads/object.txt" {
		t.Fatalf("storage path = %q, want %q", found.StoragePath, "uploads/object.txt")
	}
	if found.Size != 15 {
		t.Fatalf("size = %d, want %d", found.Size, 15)
	}

	if _, err := repo.FindFileObjectByHash("missing-hash"); !errors.Is(err, ErrFileObjectNotFound) {
		t.Fatalf("missing object error = %v, want %v", err, ErrFileObjectNotFound)
	}
}
