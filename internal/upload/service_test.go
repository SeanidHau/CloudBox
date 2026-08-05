package upload

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	filemodule "github.com/SeanidHau/CloudBox/internal/file"
	"github.com/SeanidHau/CloudBox/internal/storage"
)

func newTestService(t *testing.T) *Service {
	t.Helper()

	repo := newTestRepository(t)
	baseDir := t.TempDir()
	fileService := filemodule.NewService(
		filemodule.NewRepository(repo.db),
		storage.NewLocalStorage(filepath.Join(baseDir, "files")),
	)

	return NewService(repo, filepath.Join(baseDir, "upload-tmp"), fileService)
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

func TestServiceUploadChunk(t *testing.T) {
	service := newTestService(t)
	task, err := service.Init(1, "video.mp4", "video/mp4", 25, 10, "")
	if err != nil {
		t.Fatalf("init upload: %v", err)
	}

	firstContent := "0123456789"
	first, err := service.UploadChunk(1, task.ID, 0, strings.NewReader(firstContent))
	if err != nil {
		t.Fatalf("upload first chunk: %v", err)
	}
	if first.Number != 0 || first.Size != 10 {
		t.Fatalf("first chunk = %#v, want number 0 and size 10", first)
	}

	expectedHash := sha256.Sum256([]byte(firstContent))
	if !first.Hash.Valid || first.Hash.String != hex.EncodeToString(expectedHash[:]) {
		t.Fatalf("chunk hash = %#v, want %q", first.Hash, hex.EncodeToString(expectedHash[:]))
	}

	chunkPath := filepath.Join(task.TempDir, "000000.part")
	savedContent, err := os.ReadFile(chunkPath)
	if err != nil {
		t.Fatalf("read first chunk: %v", err)
	}
	if string(savedContent) != firstContent {
		t.Fatalf("saved chunk = %q, want %q", string(savedContent), firstContent)
	}

	updatedContent := "abcdefghij"
	if _, err := service.UploadChunk(1, task.ID, 0, strings.NewReader(updatedContent)); err != nil {
		t.Fatalf("reupload first chunk: %v", err)
	}
	savedContent, err = os.ReadFile(chunkPath)
	if err != nil {
		t.Fatalf("read replaced chunk: %v", err)
	}
	if string(savedContent) != updatedContent {
		t.Fatalf("replaced chunk = %q, want %q", string(savedContent), updatedContent)
	}

	last, err := service.UploadChunk(1, task.ID, 2, strings.NewReader("12345"))
	if err != nil {
		t.Fatalf("upload last chunk: %v", err)
	}
	if last.Size != 5 {
		t.Fatalf("last chunk size = %d, want 5", last.Size)
	}

	chunks, err := service.repo.ListChunks(task.ID)
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(chunks))
	}
}

func TestServiceUploadChunkValidatesTaskAndSize(t *testing.T) {
	service := newTestService(t)
	task, err := service.Init(1, "video.mp4", "video/mp4", 25, 10, "")
	if err != nil {
		t.Fatalf("init upload: %v", err)
	}

	if _, err := service.UploadChunk(1, "missing-task", 0, strings.NewReader("0123456789")); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task error = %v, want %v", err, ErrTaskNotFound)
	}
	if _, err := service.UploadChunk(1, task.ID, -1, strings.NewReader("0123456789")); !errors.Is(err, ErrChunkNumberInvalid) {
		t.Fatalf("negative chunk number error = %v, want %v", err, ErrChunkNumberInvalid)
	}
	if _, err := service.UploadChunk(1, task.ID, 3, strings.NewReader("0123456789")); !errors.Is(err, ErrChunkNumberInvalid) {
		t.Fatalf("out of range chunk number error = %v, want %v", err, ErrChunkNumberInvalid)
	}
	if _, err := service.UploadChunk(1, task.ID, 1, strings.NewReader("too-short")); !errors.Is(err, ErrChunkSizeMismatch) {
		t.Fatalf("wrong chunk size error = %v, want %v", err, ErrChunkSizeMismatch)
	}

	if _, err := os.Stat(filepath.Join(task.TempDir, "000001.part")); !os.IsNotExist(err) {
		t.Fatalf("invalid chunk should not be kept, stat error = %v", err)
	}

	if _, err := service.repo.db.Exec(`UPDATE upload_tasks SET status = ? WHERE id = ?`, StatusCompleted, task.ID); err != nil {
		t.Fatalf("mark task completed: %v", err)
	}
	if _, err := service.UploadChunk(1, task.ID, 1, strings.NewReader("0123456789")); !errors.Is(err, ErrTaskNotUploading) {
		t.Fatalf("completed task error = %v, want %v", err, ErrTaskNotUploading)
	}
}

func TestServiceGetStatus(t *testing.T) {
	service := newTestService(t)
	task, err := service.Init(1, "video.mp4", "video/mp4", 25, 10, "")
	if err != nil {
		t.Fatalf("init upload: %v", err)
	}

	if _, err := service.UploadChunk(1, task.ID, 1, strings.NewReader("abcdefghij")); err != nil {
		t.Fatalf("upload chunk 1: %v", err)
	}
	if _, err := service.UploadChunk(1, task.ID, 0, strings.NewReader("0123456789")); err != nil {
		t.Fatalf("upload chunk 0: %v", err)
	}

	status, err := service.GetStatus(1, task.ID)
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if status.Upload.ID != task.ID {
		t.Fatalf("task ID = %q, want %q", status.Upload.ID, task.ID)
	}
	if len(status.Chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(status.Chunks))
	}
	if status.Chunks[0].Number != 0 || status.Chunks[1].Number != 1 {
		t.Fatalf("chunk order = %d, %d, want 0, 1", status.Chunks[0].Number, status.Chunks[1].Number)
	}

	if _, err := service.GetStatus(2, task.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("other user error = %v, want %v", err, ErrTaskNotFound)
	}
}

func TestServiceCompleteCreatesFileAndCleansTemporaryChunks(t *testing.T) {
	service := newTestService(t)
	content := []string{"0123456789", "abcdefghij", "12345"}
	fullContent := strings.Join(content, "")
	expectedHash := sha256.Sum256([]byte(fullContent))

	task, err := service.Init(
		1,
		"video.mp4",
		"video/mp4",
		int64(len(fullContent)),
		10,
		hex.EncodeToString(expectedHash[:]),
	)
	if err != nil {
		t.Fatalf("init upload: %v", err)
	}
	for number, part := range content {
		if _, err := service.UploadChunk(1, task.ID, int64(number), strings.NewReader(part)); err != nil {
			t.Fatalf("upload chunk %d: %v", number, err)
		}
	}

	userFile, err := service.Complete(1, task.ID)
	if err != nil {
		t.Fatalf("complete upload: %v", err)
	}
	if userFile.OriginalName != task.OriginalName || userFile.Size != task.FileSize {
		t.Fatalf("user file = %#v, want completed upload metadata", userFile)
	}

	savedContent, err := os.ReadFile(userFile.StoragePath)
	if err != nil {
		t.Fatalf("read completed file: %v", err)
	}
	if string(savedContent) != fullContent {
		t.Fatalf("completed content = %q, want %q", savedContent, fullContent)
	}

	completedTask, err := service.repo.FindByID(1, task.ID)
	if err != nil {
		t.Fatalf("find completed task: %v", err)
	}
	if completedTask.Status != StatusCompleted {
		t.Fatalf("status = %q, want %q", completedTask.Status, StatusCompleted)
	}
	if _, err := os.Stat(task.TempDir); !os.IsNotExist(err) {
		t.Fatalf("temporary directory should be removed, stat error = %v", err)
	}
}

func TestServiceCompleteAllowsOnlyOneConcurrentRequest(t *testing.T) {
	service := newTestService(t)
	task, err := service.Init(1, "video.mp4", "video/mp4", 25, 10, "")
	if err != nil {
		t.Fatalf("init upload: %v", err)
	}
	for number, content := range []string{"0123456789", "abcdefghij", "12345"} {
		if _, err := service.UploadChunk(1, task.ID, int64(number), strings.NewReader(content)); err != nil {
			t.Fatalf("upload chunk %d: %v", number, err)
		}
	}

	results := make(chan error, 2)
	var group sync.WaitGroup

	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := service.Complete(1, task.ID)
			results <- err
		}()
	}

	group.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if errors.Is(err, ErrTaskNotUploading) {
			conflicts++
			continue
		}
		t.Fatalf("complete error = %v, want success or conflict", err)
	}

	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes = %d, conflicts = %d, want 1 and 1", successes, conflicts)
	}

	files, err := filemodule.NewRepository(service.repo.db).ListActive(1)
	if err != nil {
		t.Fatalf("list active files: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("active file count = %d, want 1", len(files))
	}
}

func TestServiceCancelMarksTaskFailedAndRemovesTemporaryFiles(t *testing.T) {
	service := newTestService(t)
	task, err := service.Init(1, "video.mp4", "video/mp4", 10, 10, "")
	if err != nil {
		t.Fatalf("init upload: %v", err)
	}
	if _, err := service.UploadChunk(1, task.ID, 0, strings.NewReader("0123456789")); err != nil {
		t.Fatalf("upload chunk: %v", err)
	}

	if err := service.Cancel(1, task.ID); err != nil {
		t.Fatalf("cancel upload: %v", err)
	}

	cancelledTask, err := service.repo.FindByID(1, task.ID)
	if err != nil {
		t.Fatalf("find cancelled task: %v", err)
	}
	if cancelledTask.Status != StatusFailed {
		t.Fatalf("status = %q, want %q", cancelledTask.Status, StatusFailed)
	}
	if _, err := os.Stat(task.TempDir); !os.IsNotExist(err) {
		t.Fatalf("temporary directory should be removed, stat error = %v", err)
	}

	if _, err := service.UploadChunk(1, task.ID, 0, strings.NewReader("0123456789")); !errors.Is(err, ErrTaskNotUploading) {
		t.Fatalf("upload after cancel error = %v, want %v", err, ErrTaskNotUploading)
	}
	if err := service.Cancel(1, task.ID); !errors.Is(err, ErrTaskNotUploading) {
		t.Fatalf("second cancel error = %v, want %v", err, ErrTaskNotUploading)
	}
}

func TestServiceCompleteRequiresAllChunks(t *testing.T) {
	service := newTestService(t)
	task, err := service.Init(1, "video.mp4", "video/mp4", 25, 10, "")
	if err != nil {
		t.Fatalf("init upload: %v", err)
	}
	if _, err := service.UploadChunk(1, task.ID, 0, strings.NewReader("0123456789")); err != nil {
		t.Fatalf("upload chunk: %v", err)
	}

	if _, err := service.Complete(1, task.ID); !errors.Is(err, ErrChunksIncomplete) {
		t.Fatalf("complete error = %v, want %v", err, ErrChunksIncomplete)
	}

	updatedTask, err := service.repo.FindByID(1, task.ID)
	if err != nil {
		t.Fatalf("find task after failed completion: %v", err)
	}
	if updatedTask.Status != StatusUploading {
		t.Fatalf("status after failed completion = %q, want %q", updatedTask.Status, StatusUploading)
	}
}

func TestServiceCompleteSetsCompletingBeforeCreatingFile(t *testing.T) {
	repo := newTestRepository(t)
	uploader := &statusCheckingUploader{repo: repo}
	service := NewService(repo, filepath.Join(t.TempDir(), "upload-tmp"), uploader)

	task, err := service.Init(1, "video.mp4", "video/mp4", 10, 10, "")
	if err != nil {
		t.Fatalf("init upload: %v", err)
	}
	uploader.userID = 1
	uploader.taskID = task.ID

	if _, err := service.UploadChunk(1, task.ID, 0, strings.NewReader("0123456789")); err != nil {
		t.Fatalf("upload chunk: %v", err)
	}
	if _, err := service.Complete(1, task.ID); err != nil {
		t.Fatalf("complete upload: %v", err)
	}
	if uploader.observedStatus != StatusCompleting {
		t.Fatalf("status during file creation = %q, want %q", uploader.observedStatus, StatusCompleting)
	}
}

type statusCheckingUploader struct {
	repo           *Repository
	userID         int64
	taskID         string
	observedStatus string
}

func (u *statusCheckingUploader) Upload(
	userID int64,
	originalName string,
	contentType string,
	reader io.Reader,
) (*filemodule.UserFile, error) {
	task, err := u.repo.FindByID(u.userID, u.taskID)
	if err != nil {
		return nil, err
	}
	u.observedStatus = task.Status

	return &filemodule.UserFile{
		UserID:       userID,
		OriginalName: originalName,
		ContentType:  contentType,
	}, nil
}
