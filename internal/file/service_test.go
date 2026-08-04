package file

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SeanidHau/CloudBox/internal/database"
)

type fakeStorage struct {
	savedPath    string
	savedContent string
	deletedPath  string
	saveErr      error
	openErr      error
}

type fakeReadSeekCloser struct {
	*strings.Reader
}

func (r fakeReadSeekCloser) Close() error {
	return nil
}

func (s *fakeStorage) Save(reader io.Reader, originalName string) (string, int64, string, error) {
	if s.saveErr != nil {
		return "", 0, "", s.saveErr
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		return "", 0, "", err
	}

	s.savedContent = string(content)
	s.savedPath = "uploads/" + originalName

	hash := sha256.Sum256(content)
	return s.savedPath, int64(len(content)), hex.EncodeToString(hash[:]), nil
}

func (s *fakeStorage) Open(storagePath string) (io.ReadSeekCloser, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}

	if storagePath != s.savedPath {
		return nil, errors.New("unexpected storage path")
	}

	return fakeReadSeekCloser{
		Reader: strings.NewReader(s.savedContent),
	}, nil
}

func (s *fakeStorage) Delete(storagePath string) error {
	s.deletedPath = storagePath
	return nil
}

func newTestServiceWithStorage(t *testing.T, storage Storage) *Service {
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

	return NewService(NewRepository(db), storage)
}

func TestServiceUploadAndOpenForDownload(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	uploaded, err := service.Upload(1, "test.txt", "text/plain", strings.NewReader("hello cloudbox"))
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if uploaded.StoragePath != storage.savedPath {
		t.Fatalf("storage path = %q, want %q", uploaded.StoragePath, storage.savedPath)
	}
	if uploaded.Size != int64(len("hello cloudbox")) {
		t.Fatalf("size = %d, want %d", uploaded.Size, len("hello cloudbox"))
	}

	userFile, reader, err := service.OpenForDownload(1, uploaded.ID)
	if err != nil {
		t.Fatalf("open for download: %v", err)
	}
	defer reader.Close()

	if userFile.ID != uploaded.ID {
		t.Fatalf("download file id = %d, want %d", userFile.ID, uploaded.ID)
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read downloaded content: %v", err)
	}
	if string(content) != "hello cloudbox" {
		t.Fatalf("downloaded content = %q, want %q", string(content), "hello cloudbox")
	}
}

func TestServiceUploadValidatesInput(t *testing.T) {
	service := newTestServiceWithStorage(t, &fakeStorage{})

	if _, err := service.Upload(1, "", "text/plain", strings.NewReader("content")); !errors.Is(err, ErrOriginalNameRequired) {
		t.Fatalf("empty original name error = %v, want %v", err, ErrOriginalNameRequired)
	}

	if _, err := service.Upload(1, "test.txt", "text/plain", nil); !errors.Is(err, ErrContentRequired) {
		t.Fatalf("nil content error = %v, want %v", err, ErrContentRequired)
	}
}

func TestServiceUploadDeduplicatesFileObjects(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	first, err := service.Upload(1, "first.txt", "text/plain", strings.NewReader("same content"))
	if err != nil {
		t.Fatalf("upload first file: %v", err)
	}
	second, err := service.Upload(1, "second.txt", "text/plain", strings.NewReader("same content"))
	if err != nil {
		t.Fatalf("upload second file: %v", err)
	}

	var objectCount int
	if err := service.repo.db.QueryRow(`SELECT COUNT(*) FROM file_objects`).Scan(&objectCount); err != nil {
		t.Fatalf("count file objects: %v", err)
	}
	if objectCount != 1 {
		t.Fatalf("file object count = %d, want 1", objectCount)
	}

	var firstObjectID int64
	if err := service.repo.db.QueryRow(`SELECT object_id FROM user_files WHERE id = ?`, first.ID).Scan(&firstObjectID); err != nil {
		t.Fatalf("query first object ID: %v", err)
	}
	var secondObjectID int64
	if err := service.repo.db.QueryRow(`SELECT object_id FROM user_files WHERE id = ?`, second.ID).Scan(&secondObjectID); err != nil {
		t.Fatalf("query second object ID: %v", err)
	}
	if firstObjectID == 0 || firstObjectID != secondObjectID {
		t.Fatalf("object IDs = %d and %d, want one shared object", firstObjectID, secondObjectID)
	}

	var referenceCount int
	if err := service.repo.db.QueryRow(`SELECT reference_count FROM file_objects WHERE id = ?`, firstObjectID).Scan(&referenceCount); err != nil {
		t.Fatalf("query reference count: %v", err)
	}
	if referenceCount != 2 {
		t.Fatalf("reference count = %d, want 2", referenceCount)
	}

	if storage.deletedPath != "uploads/second.txt" {
		t.Fatalf("deleted path = %q, want %q", storage.deletedPath, "uploads/second.txt")
	}
}

func TestServiceInstantUpload(t *testing.T) {
	storage := &fakeStorage{}
	service := newTestServiceWithStorage(t, storage)

	const fileHash = "a9c2a8c997d2a80c4756e14b6c80e7a5ed8f0262ba1e430ac0c0e751ea0b3abe"
	object, err := service.repo.CreateFileObject(fileHash, "uploads/original.txt", 15, "text/plain")
	if err != nil {
		t.Fatalf("create file object: %v", err)
	}

	created, err := service.InstantUpload(1, "instant-copy.txt", fileHash)
	if err != nil {
		t.Fatalf("instant upload: %v", err)
	}
	if created.OriginalName != "instant-copy.txt" {
		t.Fatalf("original name = %q, want %q", created.OriginalName, "instant-copy.txt")
	}
	if created.StoragePath != object.StoragePath {
		t.Fatalf("storage path = %q, want %q", created.StoragePath, object.StoragePath)
	}
	if storage.savedPath != "" {
		t.Fatalf("instant upload should not save a file, got %q", storage.savedPath)
	}

	var referenceCount int
	if err := service.repo.db.QueryRow(`SELECT reference_count FROM file_objects WHERE id = ?`, object.ID).Scan(&referenceCount); err != nil {
		t.Fatalf("query reference count: %v", err)
	}
	if referenceCount != 1 {
		t.Fatalf("reference count = %d, want 1", referenceCount)
	}

	if _, err := service.InstantUpload(1, "copy.txt", "missing-hash"); !errors.Is(err, ErrFileObjectNotFound) {
		t.Fatalf("missing hash error = %v, want %v", err, ErrFileObjectNotFound)
	}
	if _, err := service.InstantUpload(1, "copy.txt", ""); !errors.Is(err, ErrFileHashRequired) {
		t.Fatalf("empty hash error = %v, want %v", err, ErrFileHashRequired)
	}
}
