package upload

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newTestService(t *testing.T) *Service {
	t.Helper()

	return NewService(
		newTestRepository(t),
		filepath.Join(t.TempDir(), "upload-tmp"),
	)
}

func TestServiceInitCreatesUploadTask(t *testing.T) {
	service := newTestService(t)

	task, err := service.Init(
		1,
		" video.mp4 ",
		"",
		25,
		10,
		" file-hash ",
	)
	if err != nil {
		t.Fatalf("init upload: %v", err)
	}
	if task.ID == "" {
		t.Fatal("expected task ID")
	}
	if task.OriginalName != "video.mp4" {
		t.Fatalf("original name = %q, want %q", task.OriginalName, "video.mp4")
	}
	if task.ContentType != "application/octet-stream" {
		t.Fatalf("content type = %q, want %q", task.ContentType, "application/octet-stream")
	}
	if task.TotalChunks != 3 {
		t.Fatalf("total chunks = %d, want 3", task.TotalChunks)
	}
	if !task.FileHash.Valid || task.FileHash.String != "file-hash" {
		t.Fatalf("file hash = %#v, want file-hash", task.FileHash)
	}
	if task.Status != StatusUploading {
		t.Fatalf("status = %q, want %q", task.Status, StatusUploading)
	}
	if info, err := os.Stat(task.TempDir); err != nil || !info.IsDir() {
		t.Fatalf("temp directory was not created: %v", err)
	}

	found, err := service.repo.FindByID(1, task.ID)
	if err != nil {
		t.Fatalf("find created task: %v", err)
	}
	if found.ID != task.ID {
		t.Fatalf("found task ID = %q, want %q", found.ID, task.ID)
	}
}

func TestServiceInitValidatesInput(t *testing.T) {
	service := newTestService(t)

	if _, err := service.Init(1, "", "text/plain", 10, 5, ""); !errors.Is(err, ErrOriginalNameRequired) {
		t.Fatalf("empty name error = %v, want %v", err, ErrOriginalNameRequired)
	}
	if _, err := service.Init(1, "file.txt", "text/plain", 0, 5, ""); !errors.Is(err, ErrFileSizeInvalid) {
		t.Fatalf("invalid file size error = %v, want %v", err, ErrFileSizeInvalid)
	}
	if _, err := service.Init(1, "file.txt", "text/plain", 10, 0, ""); !errors.Is(err, ErrChunkSizeInvalid) {
		t.Fatalf("invalid chunk size error = %v, want %v", err, ErrChunkSizeInvalid)
	}
}
