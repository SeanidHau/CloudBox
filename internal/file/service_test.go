package file

import (
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

func (s *fakeStorage) Save(reader io.Reader, originalName string) (string, int64, error) {
	if s.saveErr != nil {
		return "", 0, s.saveErr
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		return "", 0, err
	}

	s.savedContent = string(content)
	s.savedPath = "uploads/" + originalName

	return s.savedPath, int64(len(content)), nil
}

func (s *fakeStorage) Open(storagePath string) (io.ReadCloser, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}

	if storagePath != s.savedPath {
		return nil, errors.New("unexpected storage path")
	}

	return io.NopCloser(strings.NewReader(s.savedContent)), nil
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

	if err := database.Migrate(db, "../../migrations/001_init.sql"); err != nil {
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
